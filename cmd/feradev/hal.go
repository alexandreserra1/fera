package main

import (
	"sync/atomic"
	"time"

	"github.com/ale/fera/internal/device/hal"
)

// macHAL é a placa, no Mac.
//
// O relógio é VIRTUAL e acelerado: o loop pede pra dormir 5 minutos e o
// programa dorme 5 minutos divididos pela velocidade, avançando o relógio
// virtual pelos 5 minutos cheios. É isso que permite ver um mês de decaimento
// em meio minuto e calibrar o balanceamento de verdade.
//
// Só o goroutine do loop mexe no relógio; as teclas chegam por canal e o nível
// de bateria por atômico. Sem lock, e o -race confirma.
type macHAL struct {
	virtual    time.Time
	velocidade float64

	teclas    chan hal.Botao
	pendentes []hal.Botao

	nivel atomic.Int32
	saida atomic.Bool
}

// Saiu é lido pelo laço principal a cada volta. Fica num atômico porque quem
// escreve é o goroutine do teclado.
func (m *macHAL) Saiu() bool { return m.saida.Load() }

func novoMacHAL(velocidade float64) *macHAL {
	if velocidade <= 0 {
		velocidade = 1
	}
	m := &macHAL{
		virtual:    time.Now().UTC().Truncate(time.Second),
		velocidade: velocidade,
		teclas:     make(chan hal.Botao, 16),
	}
	m.nivel.Store(100)
	return m
}

func (m *macHAL) Now() time.Time { return m.virtual }

func (m *macHAL) Bateria() uint8 { return uint8(m.nivel.Load()) }

func (m *macHAL) tecla(b hal.Botao) {
	select {
	case m.teclas <- b:
	default: // fila cheia: dedo mais rápido que o loop, descarta
	}
}

func (m *macHAL) baixarBateria() {
	n := m.nivel.Add(-10)
	if n < 0 {
		m.nivel.Store(0)
	}
}

func (m *macHAL) pedirSaida() { m.saida.Store(true) }

// Sleep dorme o tempo real correspondente e avança o relógio virtual pelo
// tempo pedido. Tecla interrompe, igual ao wake por interrupção no ESP32.
func (m *macHAL) Sleep(d time.Duration) hal.Motivo {
	real := time.Duration(float64(d) / m.velocidade)
	if real > 200*time.Millisecond {
		// Fatia o sono pra que tecla e Ctrl-C respondam rápido mesmo com o
		// relógio devagar. Sem isso, velocidade=1 travaria o terminal por 5
		// minutos entre um tick e outro.
		return m.dormirFatiado(d, real)
	}
	inicio := time.Now()
	select {
	case b := <-m.teclas:
		m.avancarProporcional(d, real, time.Since(inicio))
		m.pendentes = append(m.pendentes, b)
		return hal.MotivoBotao
	case <-time.After(real):
		m.virtual = m.virtual.Add(d)
		return hal.MotivoTimer
	}
}

func (m *macHAL) dormirFatiado(virtual, real time.Duration) hal.Motivo {
	const fatia = 100 * time.Millisecond
	gasto := time.Duration(0)
	for gasto < real {
		if m.saida.Load() {
			return hal.MotivoDesligar
		}
		passo := fatia
		if restante := real - gasto; restante < passo {
			passo = restante
		}
		select {
		case b := <-m.teclas:
			m.avancarProporcional(virtual, real, gasto)
			m.pendentes = append(m.pendentes, b)
			return hal.MotivoBotao
		case <-time.After(passo):
			gasto += passo
		}
	}
	m.virtual = m.virtual.Add(virtual)
	return hal.MotivoTimer
}

// avancarProporcional move o relógio virtual pela fração do sono que
// realmente passou. Sem isso, apertar tecla logo depois de acordar
// congelaria o tempo do bicho.
func (m *macHAL) avancarProporcional(virtual, real, decorrido time.Duration) {
	if real <= 0 {
		m.virtual = m.virtual.Add(virtual)
		return
	}
	frac := float64(decorrido) / float64(real)
	if frac > 1 {
		frac = 1
	}
	m.virtual = m.virtual.Add(time.Duration(float64(virtual) * frac))
}

func (m *macHAL) Botoes() []hal.Botao {
	// drena o que chegou fora da janela de sono também
	for {
		select {
		case b := <-m.teclas:
			m.pendentes = append(m.pendentes, b)
		default:
			goto pronto
		}
	}
pronto:
	if len(m.pendentes) == 0 {
		return nil
	}
	out := m.pendentes
	m.pendentes = nil
	return out
}

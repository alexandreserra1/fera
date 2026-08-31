// Package loop é a máquina de estados de energia do device.
//
// Mora aqui e não em cmd/firmware por um motivo prático: as regras difíceis
// deste projeto são "quando gravar" e "quando desenhar", e lógica dentro de
// main não tem como ser testada. Os dois binários (firmware e feradev) só
// fazem wiring.
//
// O laço é dirigido por EVENTO, não por tick. Com memory LCD a imagem fica na
// tela sem energia, então redesenhar em laço é desperdício puro.
package loop

import (
	"time"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/hal"
	"github.com/ale/fera/internal/device/input"
	"github.com/ale/fera/internal/device/store"
	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/device/ulid"
	"github.com/ale/fera/internal/sim"
)

// Sincronizador é a costura com a rede: um método, e o loop não sabe se do
// outro lado tem HTTP, BLE ou nada.
type Sincronizador interface {
	Sync(pendentes []sim.Event) (enviados int, err error)
}

// Tela é o que o loop precisa do display: onde desenhar e como mandar pro
// vidro. Quem implementa isso hoje é o display.Fake; o driver de verdade
// entra sem tocar aqui.
type Tela interface {
	Framebuffer() *display.Buffer
	Show() error
}

type Config struct {
	PetID  string
	Tuning sim.Tuning

	// Ocioso é de quanto em quanto tempo o device acorda sem ninguém mexer.
	Ocioso time.Duration
	// DuracaoAtiva e PassoAtivo: depois de um botão o device fica acordando
	// rápido por um tempo. É o que vai sustentar a animação do docs/06
	// (10 fps por 15 s quando o dono interage).
	//
	// DuracaoAtiva zero desliga o modo ativo, e é o PADRÃO hoje: sem
	// ui.Animate, essas acordadas desenham o mesmo frame. Medido em
	// TestCustoDoModoAtivoSemAnimacao: 750 acordadas a mais por dia pra
	// ZERO redesenho a mais. Liga junto com a animação, não antes.
	DuracaoAtiva time.Duration
	PassoAtivo   time.Duration

	IntervaloSync  time.Duration
	BateriaCritica uint8

	// Entropia alimenta o ULID. Injetada pra que o teste seja determinístico
	// e o device use o RNG de hardware.
	Entropia func() [10]byte
}

func Padrao() Config {
	var n uint32
	return Config{
		Tuning:         sim.DefaultTuning(),
		Ocioso:         5 * time.Minute,
		DuracaoAtiva:   0,                      // ver o comentário no campo
		PassoAtivo:     100 * time.Millisecond, // 10 fps, quando ligar
		IntervaloSync:  12 * time.Hour,         // 1 a 2x por dia, o wifi come bateria
		BateriaCritica: 15,
		// Contador, não aleatório: o Padrao existe pra teste e pra bancada.
		// Quem vai pra campo passa o RNG de hardware.
		Entropia: func() [10]byte {
			n++
			return [10]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
		},
	}
}

type Loop struct {
	h    hal.HAL
	tela Tela
	st   *store.Store
	sinc Sincronizador
	cfg  Config

	estado sim.State
	vista  sim.View
	fila   *input.Fila

	fimAtivo      time.Time
	proxSync      time.Time
	desenhou      bool
	avisouBateria bool

	// Buffer de eventos pré-alocado: no máximo um por botão por acordada.
	// Nenhum make no laço, senão o GC conservativo do TinyGo acorda junto.
	evs [8]sim.Event

	// PerdidosNaFila conta evento que não coube nem depois de forçar sync.
	// Alto e visível de propósito: é divergência permanente com o servidor.
	PerdidosNaFila int
}

// New recupera o device. Estado na flash é o caso normal de todo boot menos o
// primeiro; flash virgem nasce um bicho novo.
func New(h hal.HAL, tela Tela, st *store.Store, sinc Sincronizador, cfg Config) (*Loop, error) {
	l := &Loop{
		h: h, tela: tela, st: st, sinc: sinc, cfg: cfg,
		fila: input.NewFila(input.JanelaPadrao),
	}

	estado, err := st.LoadState()
	if err == store.ErrVazio {
		estado = sim.Genesis(cfg.PetID, h.Now())
	} else if err != nil {
		return nil, err
	}

	l.estado = estado
	l.proxSync = h.Now().Add(cfg.IntervaloSync)
	// A vista projeta pra AGORA, então o tempo em que o device ficou
	// desligado entra como decaimento. O bicho não congela na gaveta.
	l.vista = sim.Project(estado, h.Now(), cfg.Tuning)
	return l, nil
}

func (l *Loop) Estado() sim.State { return l.estado }
func (l *Loop) Vista() sim.View   { return l.vista }

// Passo é UMA volta do laço. Público pra que o teste possa dar passos
// contados em vez de precisar interromper um laço infinito.
func (l *Loop) Passo() error {
	motivo := l.h.Sleep(l.espera())
	agora := l.h.Now()

	evs := l.colhe(agora)
	gravar := false

	if len(evs) > 0 {
		if err := l.enfileira(evs); err != nil {
			return err
		}
		l.fimAtivo = agora.Add(l.cfg.DuracaoAtiva)
		gravar = true
	}

	anterior := l.estado
	l.estado = sim.Fold(l.estado, evs, l.cfg.Tuning)
	if l.estado.Stage != anterior.Stage {
		gravar = true // evoluiu: momento que vale gastar um ciclo de flash
	}

	// Bateria crítica grava UMA vez. Gravar a cada tick com a bateria baixa
	// seria justamente a escrita em laço que mata a flash.
	if l.h.Bateria() <= l.cfg.BateriaCritica {
		if !l.avisouBateria {
			l.avisouBateria = true
			gravar = true
		}
	} else {
		l.avisouBateria = false
	}

	if !agora.Before(l.proxSync) {
		// Erro de sync não é fatal: o device é autoridade local e tenta de
		// novo no próximo intervalo. Sem rede o bicho continua vivo.
		_ = l.sincroniza()
		l.proxSync = agora.Add(l.cfg.IntervaloSync)
	}

	l.desenha(agora)

	if gravar {
		if err := l.st.SaveState(l.estado); err != nil {
			return err
		}
	}
	_ = motivo
	return nil
}

// Rodar é o laço de verdade. parar existe pro Mac poder sair no Ctrl-C; no
// device ele é sempre nil e o laço não termina.
func (l *Loop) Rodar(parar func() bool) error {
	for {
		if parar != nil && parar() {
			return nil
		}
		if err := l.Passo(); err != nil {
			return err
		}
	}
}

// espera decide quanto dormir. É a máquina de estados inteira em cinco linhas:
// depois de um botão o device responde rápido por um tempo, e o resto do dia
// ele dorme.
func (l *Loop) espera() time.Duration {
	if l.h.Now().Before(l.fimAtivo) {
		return l.cfg.PassoAtivo
	}
	return l.cfg.Ocioso
}

// colhe transforma botão em evento, usando o buffer fixo do Loop.
func (l *Loop) colhe(agora time.Time) []sim.Event {
	l.fila.Alimentar(l.h.Botoes(), agora)
	botoes := l.fila.Drenar()

	n := 0
	for _, b := range botoes {
		if n >= len(l.evs) {
			break
		}
		kind, ok := kindDoBotao(b)
		if !ok {
			continue
		}
		l.evs[n] = sim.Event{
			ID:      ulid.New(agora, l.cfg.Entropia()),
			At:      agora,
			Kind:    kind,
			Kcal:    kcalDoBotao(b),
			Zone:    zonaDoBotao(b),
			Minutes: minutosDoBotao(b),
		}
		n++
	}
	return l.evs[:n]
}

// O botão de menu não gera evento: navegar não é interagir com o bicho.
func kindDoBotao(b hal.Botao) (sim.Kind, bool) {
	switch b {
	case hal.BotaoInteragir:
		return sim.KindInteract, true
	case hal.BotaoAlimentar:
		// "Alimentar" no device é registrar esforço na mão, pro dia 1
		// funcionar sem depender de wearable nenhum. Ver docs/05, integrações.
		return sim.KindEffort, true
	}
	return 0, false
}

// TODO: calibrar. Enquanto o device não lê IMU nem wearable, alimentar na mão
// vale um treino leve fixo. Número chutado de propósito e marcado como tal.
func kcalDoBotao(b hal.Botao) uint16 {
	if b == hal.BotaoAlimentar {
		return 150
	}
	return 0
}

func zonaDoBotao(b hal.Botao) uint8 {
	if b == hal.BotaoAlimentar {
		return 2
	}
	return 0
}

func minutosDoBotao(hal.Botao) uint16 { return 0 }

// enfileira grava os eventos na fila de sync. Fila cheia força um sync e
// tenta de novo: descartar em silêncio seria perder treino que o dono fez.
func (l *Loop) enfileira(evs []sim.Event) error {
	for _, ev := range evs {
		err := l.st.AppendPending(ev)
		if err == store.ErrFilaCheia {
			if e := l.sincroniza(); e != nil {
				l.PerdidosNaFila++
				continue
			}
			err = l.st.AppendPending(ev)
			if err == store.ErrFilaCheia {
				l.PerdidosNaFila++
				continue
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// sincroniza empurra os pendentes. Só marca como enviado o que o outro lado
// confirmou: reenviar é seguro por causa do ULID, perder não é.
//
// A ORDEM aqui não é arbitrária. MarkSynced descarta os eventos da fila local,
// e o estado em flash pode ser anterior a eles. Se a energia cair entre
// descartar e gravar, o device reboota com um estado velho e sem os eventos
// pra refazer o caminho: o treino sumiu do device mesmo já estando no
// servidor. Gravar primeiro custa um ciclo de flash e fecha essa janela.
//
// Sync sem nada pendente não grava nada. Estado só muda por evento, então
// gravar num sync vazio seria queimar flash por nada, duas vezes ao dia.
func (l *Loop) sincroniza() error {
	pend, err := l.st.Pending()
	if err != nil {
		return err
	}
	if len(pend) == 0 {
		return nil
	}
	enviados, err := l.sinc.Sync(pend)
	if err != nil {
		return err
	}
	if err := l.st.SaveState(l.estado); err != nil {
		return err
	}
	return l.st.MarkSynced(enviados)
}

// desenha só quando o que o DONO enxerga mudou.
//
// A comparação é sobre sim.View, não sobre sim.State: a tela mostra atributo
// em 0..100, então decaimento que não move nenhum desses inteiros é
// invisível. Comparar o State cru redesenharia a cada tick, e desenhar é o
// que gasta bateria.
func (l *Loop) desenha(agora time.Time) {
	v := sim.Project(l.estado, agora, l.cfg.Tuning)
	if l.desenhou && v == l.vista {
		return
	}
	l.vista = v
	l.desenhou = true
	ui.Render(l.tela.Framebuffer(), v)
	_ = l.tela.Show()
}

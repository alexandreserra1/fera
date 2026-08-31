// Package hal é a fronteira com o hardware.
//
// É o ÚNICO pacote do device que um dia vai importar "machine". Essa regra é
// o que faz `sim`, `ui`, `store`, `input` e `loop` rodarem com `go test` no
// Mac: tudo que precisa de placa está atrás desta interface, e o Fake aqui
// substitui a placa inteira.
package hal

import "time"

// Motivo diz por que o device acordou. Ele decide o que o loop faz em
// seguida: timer é rotina, botão é interação e merece animação.
type Motivo uint8

const (
	MotivoTimer Motivo = iota
	MotivoBotao
	MotivoDesligar
)

// Botao são os três do docs/04: alimentar, interagir, menu.
type Botao uint8

const (
	BotaoAlimentar Botao = iota
	BotaoInteragir
	BotaoMenu
)

// HAL é o mínimo que o loop precisa da placa.
//
// Quatro métodos, que é o teto do projeto, e cada um existe por um motivo do
// docs/06: Now porque o decaimento depende de tempo real, Sleep porque deep
// sleep é o estado padrão e não a exceção, Botoes porque interação acorda o
// device, e Bateria porque "antes de dormir por bateria crítica" é um dos
// quatro gatilhos de gravação em flash.
type HAL interface {
	Now() time.Time
	// Sleep dorme até d passar OU até um botão acordar, o que vier primeiro.
	Sleep(d time.Duration) Motivo
	// Botoes drena o que foi apertado desde a última leitura.
	Botoes() []Botao
	Bateria() uint8 // 0..100
}

type agendado struct {
	quando time.Time
	botao  Botao
}

// Fake é a placa inteira, em memória e sob controle do teste. Semanas passam
// em microssegundos, e botão apertado é uma linha de código.
type Fake struct {
	agora     time.Time
	agenda    []agendado
	pendentes []Botao

	Nivel        uint8 // bateria, mexa à vontade
	Sonos        int
	TempoDormido time.Duration
}

func NewFake(inicio time.Time) *Fake {
	return &Fake{agora: inicio, Nivel: 100}
}

func (f *Fake) Now() time.Time { return f.agora }

func (f *Fake) Bateria() uint8 { return f.Nivel }

// Agendar marca um aperto de botão pra daqui a d. É assim que o teste diz
// "o dono interage às 7 da manhã" sem esperar até as 7 da manhã.
func (f *Fake) Agendar(d time.Duration, b Botao) {
	f.inserir(agendado{f.agora.Add(d), b})
}

// AgendarEm marca um aperto num instante absoluto.
func (f *Fake) AgendarEm(quando time.Time, b Botao) {
	f.inserir(agendado{quando, b})
}

// insertion sort por instante: a agenda é curta e sort.Slice puxaria reflect,
// que é o que este projeto evita em tudo que pode acabar no device.
func (f *Fake) inserir(a agendado) {
	f.agenda = append(f.agenda, a)
	for i := len(f.agenda) - 1; i > 0 && f.agenda[i].quando.Before(f.agenda[i-1].quando); i-- {
		f.agenda[i], f.agenda[i-1] = f.agenda[i-1], f.agenda[i]
	}
}

// Sleep avança o relógio, parando cedo se um botão agendado cair dentro da
// janela. Interromper o sono é o comportamento do wake por interrupção no
// ESP32, e é o que faz o bicho responder ao toque na hora em vez de daqui a
// cinco minutos.
func (f *Fake) Sleep(d time.Duration) Motivo {
	f.Sonos++
	limite := f.agora.Add(d)

	if len(f.agenda) > 0 && !f.agenda[0].quando.After(limite) {
		prox := f.agenda[0]
		f.TempoDormido += prox.quando.Sub(f.agora)
		f.agora = prox.quando
		f.agenda = f.agenda[1:]
		f.pendentes = append(f.pendentes, prox.botao)
		return MotivoBotao
	}

	f.TempoDormido += d
	f.agora = limite
	return MotivoTimer
}

// Botoes drena. Ler duas vezes não devolve o mesmo aperto: senão o loop
// geraria dois eventos pro mesmo toque, e evento duplicado por bug de leitura
// não é o tipo de duplicata que a idempotência por ULID resolve.
func (f *Fake) Botoes() []Botao {
	if len(f.pendentes) == 0 {
		return nil
	}
	out := f.pendentes
	f.pendentes = nil
	return out
}

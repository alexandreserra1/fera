// Package input tira o quique dos botões.
//
// Puro: recebe (botão, apertado, instante) e devolve apertos limpos. Não
// importa "machine" nem hal.Fake, então roda com `go test` no Mac e o mesmo
// código roda no device.
package input

import (
	"time"

	"github.com/ale/fera/internal/device/hal"
)

// JanelaPadrao é o tempo em que apertos do mesmo botão são considerados o
// mesmo toque. 50 ms cobre com folga o quique de um tactile de 6 mm (que dura
// poucos milissegundos) e fica bem abaixo do intervalo entre dois toques
// humanos deliberados.
const JanelaPadrao = 50 * time.Millisecond

const nBotoes = 3

// Debouncer guarda, por botão, quando ele foi aceito pela última vez e se
// continua pressionado. O segundo campo é o que distingue segurar de apertar
// várias vezes.
type Debouncer struct {
	janela  time.Duration
	ultimo  [nBotoes]time.Time
	descido [nBotoes]bool
	visto   [nBotoes]bool
}

func New(janela time.Duration) *Debouncer {
	return &Debouncer{janela: janela}
}

// Amostra processa uma leitura do pino. Devolve ok=true só na borda de
// descida limpa: apertar gera evento, soltar não, e segurar gera um só.
func (d *Debouncer) Amostra(b hal.Botao, apertado bool, agora time.Time) (hal.Botao, bool) {
	i := int(b)
	if i < 0 || i >= nBotoes {
		return b, false
	}

	if !apertado {
		// Só marca como solto depois da janela. Soltar durante o quique é
		// ruído do contato, não intenção: se aceitasse, o próximo fechamento
		// contaria como aperto novo.
		if d.visto[i] && agora.Sub(d.ultimo[i]) >= d.janela {
			d.descido[i] = false
		}
		return b, false
	}

	if d.descido[i] {
		return b, false // já está segurando: não é aperto novo
	}
	if d.visto[i] && agora.Sub(d.ultimo[i]) < d.janela {
		return b, false // quique dentro da janela
	}

	d.descido[i] = true
	d.visto[i] = true
	d.ultimo[i] = agora
	return b, true
}

// Fila é o que o loop consome. Existe porque o hal entrega os botões em lote
// depois de acordar, e o debounce precisa vê-los um a um.
//
// Buffer fixo, sem crescimento: mais de 8 apertos numa única acordada é ruído
// elétrico, não um dono muito rápido, e descartar é melhor que alocar.
type Fila struct {
	d     *Debouncer
	buf   [8]hal.Botao
	n     int
	solto bool
}

func NewFila(janela time.Duration) *Fila {
	return &Fila{d: New(janela)}
}

// Alimentar joga um lote cru do hal na fila, já filtrado pelo debounce.
func (f *Fila) Alimentar(botoes []hal.Botao, agora time.Time) {
	for _, b := range botoes {
		// O hal entrega o lote depois de acordar, então cada item já é uma
		// borda de descida. O que o debounce corta aqui é repetição do mesmo
		// botão dentro da janela.
		if _, ok := f.d.Amostra(b, true, agora); ok {
			if f.n < len(f.buf) {
				f.buf[f.n] = b
				f.n++
			}
		}
		f.d.Amostra(b, false, agora.Add(JanelaPadrao))
	}
}

// Drenar entrega o que está na fila e zera. O slice devolvido aponta pro
// buffer interno: quem chama tem que consumir antes do próximo Alimentar.
// É o preço de não alocar no laço do device.
func (f *Fila) Drenar() []hal.Botao {
	if f.n == 0 {
		return nil
	}
	out := f.buf[:f.n]
	f.n = 0
	return out
}

package input

import (
	"testing"
	"time"

	"github.com/ale/fera/internal/device/hal"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

// Sequência de quique de um contato mecânico de verdade: fecha, abre, fecha,
// abre, fecha em poucos milissegundos. Sem debounce isso vira três apertos, o
// bicho ganha três interações de um toque, e o log enche de evento inventado.
func TestQuiqueViraUmApertoSo(t *testing.T) {
	d := New(JanelaPadrao)

	quique := []struct {
		ms       int
		apertado bool
	}{
		{0, true}, {3, false}, {5, true}, {8, false}, {11, true},
		{200, false}, // soltou de verdade
	}

	n := 0
	for _, q := range quique {
		if _, ok := d.Amostra(hal.BotaoInteragir, q.apertado, t0.Add(time.Duration(q.ms)*time.Millisecond)); ok {
			n++
		}
	}
	if n != 1 {
		t.Errorf("o quique gerou %d apertos, esperado 1", n)
	}
}

func TestDoisApertosSeparadosContamDois(t *testing.T) {
	d := New(JanelaPadrao)

	solta := func(em time.Duration) { d.Amostra(hal.BotaoInteragir, false, t0.Add(em)) }
	aperta := func(em time.Duration) bool {
		_, ok := d.Amostra(hal.BotaoInteragir, true, t0.Add(em))
		return ok
	}

	if !aperta(0) {
		t.Fatal("o primeiro aperto não passou")
	}
	solta(100 * time.Millisecond)
	if !aperta(500 * time.Millisecond) {
		t.Error("o segundo aperto, bem separado do primeiro, foi engolido")
	}
}

// Segurar o botão não é apertar várias vezes. Sem isso, encostar o dedo por
// dois segundos alimentaria o bicho vinte vezes.
func TestSegurarNaoViraVariosApertos(t *testing.T) {
	d := New(JanelaPadrao)

	n := 0
	for ms := 0; ms < 2000; ms += 10 {
		if _, ok := d.Amostra(hal.BotaoAlimentar, true, t0.Add(time.Duration(ms)*time.Millisecond)); ok {
			n++
		}
	}
	if n != 1 {
		t.Errorf("segurar 2s gerou %d apertos, esperado 1", n)
	}
}

// Cada botão tem estado próprio: o quique de um não pode mascarar o aperto
// legítimo de outro.
func TestBotoesSaoIndependentes(t *testing.T) {
	d := New(JanelaPadrao)

	if _, ok := d.Amostra(hal.BotaoAlimentar, true, t0); !ok {
		t.Fatal("o aperto no alimentar não passou")
	}
	if _, ok := d.Amostra(hal.BotaoInteragir, true, t0.Add(time.Millisecond)); !ok {
		t.Error("o aperto no interagir foi engolido pela janela do alimentar")
	}
}

// Soltar não gera evento: o bicho responde a apertar, não a largar.
func TestSoltarNaoGeraEvento(t *testing.T) {
	d := New(JanelaPadrao)
	if _, ok := d.Amostra(hal.BotaoMenu, false, t0); ok {
		t.Error("soltar gerou aperto")
	}
}

// O debounce é puro: mesma sequência, mesmo resultado, sempre. Sem isso o
// device e um replay de teste divergiriam.
func TestDebounceEhDeterministico(t *testing.T) {
	roda := func() []hal.Botao {
		d := New(JanelaPadrao)
		var out []hal.Botao
		for ms := 0; ms < 500; ms += 7 {
			b, ok := d.Amostra(hal.BotaoInteragir, ms%3 != 0, t0.Add(time.Duration(ms)*time.Millisecond))
			if ok {
				out = append(out, b)
			}
		}
		return out
	}
	a, b := roda(), roda()
	if len(a) != len(b) {
		t.Fatalf("mesma sequência deu %d e %d apertos", len(a), len(b))
	}
}

// A fila é o que o loop consome. Ela existe porque o hal entrega os botões em
// lote depois de acordar, e o debounce precisa vê-los um a um.
func TestFilaDrenaNaOrdem(t *testing.T) {
	f := NewFila(JanelaPadrao)

	f.Alimentar([]hal.Botao{hal.BotaoAlimentar, hal.BotaoInteragir}, t0)
	f.Alimentar([]hal.Botao{hal.BotaoMenu}, t0.Add(time.Second))

	got := f.Drenar()
	want := []hal.Botao{hal.BotaoAlimentar, hal.BotaoInteragir, hal.BotaoMenu}
	if len(got) != len(want) {
		t.Fatalf("drenou %v, esperado %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("posição %d = %v, esperado %v", i, got[i], want[i])
		}
	}
	if rest := f.Drenar(); len(rest) != 0 {
		t.Errorf("a segunda drenagem devolveu %v, esperado vazio", rest)
	}
}

// A fila também filtra quique: dois apertos do mesmo botão no mesmo instante
// são um toque só chegando por caminhos diferentes.
func TestFilaFiltraQuique(t *testing.T) {
	f := NewFila(JanelaPadrao)
	f.Alimentar([]hal.Botao{hal.BotaoInteragir, hal.BotaoInteragir, hal.BotaoInteragir}, t0)

	if got := f.Drenar(); len(got) != 1 {
		t.Errorf("drenou %d apertos, esperado 1", len(got))
	}
}

// Alocação zero: o loop chama isto a cada acordada.
func TestFilaNaoAloca(t *testing.T) {
	f := NewFila(JanelaPadrao)
	lote := []hal.Botao{hal.BotaoInteragir}
	i := 0

	n := testing.AllocsPerRun(200, func() {
		i++
		f.Alimentar(lote, t0.Add(time.Duration(i)*time.Second))
		f.Drenar()
	})
	if n != 0 {
		t.Errorf("o par Alimentar/Drenar alocou %v vezes, esperado 0", n)
	}
}

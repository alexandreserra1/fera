package loop

import (
	"testing"
	"time"

	"github.com/ale/fera/internal/device/hal"
)

// Apertos do MESMO botão separados por segundos são dois toques de verdade,
// não quique de contato. Se o debounce colapsar isso, o dono aperta duas
// vezes e o bicho responde uma.
func TestDoisApertosDoMesmoBotaoSeparadosContamDois(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)

	b.h.Agendar(30*time.Second, hal.BotaoInteragir)
	b.h.Agendar(90*time.Second, hal.BotaoInteragir)
	b.passos(t, 2)

	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d eventos na fila", len(pend))
	if len(pend) != 2 {
		t.Errorf("dois apertos separados por 1 minuto viraram %d evento(s)", len(pend))
	}
}

// Botões DIFERENTES na mesma acordada são dois eventos: um não pode mascarar
// o outro.
func TestBotoesDiferentesNaMesmaAcordada(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.h.Agendar(time.Minute, hal.BotaoAlimentar)
	b.passos(t, 2)

	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 2 {
		t.Errorf("%d eventos, esperado 2 (interagir e alimentar)", len(pend))
	}
}

// Dois apertos do mesmo botão no MESMO instante são quique de contato, e o
// debounce tem que colapsar: humano não aperta duas vezes em 50 ms.
func TestQuiqueDoMesmoBotaoNoMesmoInstanteColapsa(t *testing.T) {
	b := nova(t, nil)
	b.passos(t, 1)

	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.h.Agendar(time.Minute, hal.BotaoInteragir)
	b.passos(t, 2)

	pend, err := b.st.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 {
		t.Errorf("%d eventos, esperado 1: quique no mesmo instante deveria colapsar", len(pend))
	}
}

func TestEntropiaPadraoNaoRepete(t *testing.T) {
	cfg := Padrao()
	vistos := map[[10]byte]bool{}
	for i := range 100 {
		e := cfg.Entropia()
		if vistos[e] {
			t.Fatalf("a entropia repetiu na chamada %d: ULIDs do mesmo instante vão colidir", i)
		}
		vistos[e] = true
	}
}

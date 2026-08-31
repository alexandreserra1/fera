package ui

import (
	"testing"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/sim"
)

func simStage(i int) sim.Stage { return sim.Stage(i) }

// Não é asserção, é vitrine: `go test -run TestOlharAFonte -v` desenha a fonte
// inteira no terminal. Fonte com typo você só descobre olhando.
func TestOlharAFonte(t *testing.T) {
	b := display.NewBuffer(112, 12)
	DrawText(b, 0, 0, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	DrawText(b, 0, 6, "0123456789 -% VIG ANI SAU")
	t.Log("\n" + b.String())
}

func TestOlharOsBichos(t *testing.T) {
	nomes := []string{"OVO", "FILHOTE", "JOVEM", "ADULTO", "VETERANO"}
	for i, n := range nomes {
		b := display.NewBuffer(larguraSprite, alturaSprite)
		b.Blit(0, 0, SpriteDoEstagio(simStage(i)))
		t.Log(n + "\n" + b.String())
	}
}

func TestOlharATelaInteira(t *testing.T) {
	casos := []struct {
		nome string
		v    sim.View
	}{
		{"recém-nascido", sim.View{Stage: sim.StageOvo, Trait: sim.TraitNeutro,
			Stats: sim.Stats{Vigor: 5000, Animo: 5000, Saude: 7000, Vinculo: 1000}}},
		{"adulto em forma", sim.View{Stage: sim.StageAdulto, Trait: sim.TraitFerino,
			Stats: sim.Stats{Vigor: 9200, Animo: 8100, Saude: 8800, Vinculo: 7400}}},
		{"largado", sim.View{Stage: sim.StageJovem, Trait: sim.TraitTeimoso,
			Stats: sim.Stats{Vigor: 0, Animo: 0, Saude: 3000, Vinculo: 0}}},
	}
	for _, c := range casos {
		d := display.NewFake(Largura, Altura)
		Render(d.Framebuffer(), c.v)
		_ = d.Show()
		t.Log(c.nome + "\n" + d.Buf.String())
	}
}

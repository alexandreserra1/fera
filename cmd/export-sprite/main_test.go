package main

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/sim"
)

// A exportação tem que ser fiel: cada pixel aceso do sprite vira preto no PNG
// e cada apagado vira branco. Se distorcer, editar por cima e reimportar
// devolveria um bicho diferente do que saiu.
func TestPngSaiFielAoSprite(t *testing.T) {
	sp := ui.SpriteDoEstagio(sim.StageAdulto)
	var b bytes.Buffer
	if err := escrever(&b, sp, 1); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&b)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != int(sp.W) || img.Bounds().Dy() != int(sp.H) {
		t.Fatalf("PNG %dx%d, sprite %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), sp.W, sp.H)
	}
	for y := int16(0); y < sp.H; y++ {
		for x := int16(0); x < sp.W; x++ {
			r, _, _, _ := img.At(int(x), int(y)).RGBA()
			escuro := r>>8 < 128
			if escuro != sp.At(x, y) {
				t.Fatalf("(%d,%d): PNG escuro=%v, sprite aceso=%v", x, y, escuro, sp.At(x, y))
			}
		}
	}
}

// Ampliar é pra conseguir ver e editar: 64x64 numa tela moderna é do tamanho
// de uma unha. A ampliação não pode inventar pixel.
func TestAmpliarMultiplicaCadaPixel(t *testing.T) {
	sp := ui.SpriteDoEstagio(sim.StageFilhote)
	var b bytes.Buffer
	if err := escrever(&b, sp, 4); err != nil {
		t.Fatal(err)
	}
	img, _ := png.Decode(&b)
	if img.Bounds().Dx() != int(sp.W)*4 {
		t.Fatalf("largura %d, esperado %d", img.Bounds().Dx(), int(sp.W)*4)
	}
	// o bloco 4x4 correspondente a um pixel tem que ser uniforme
	for _, p := range [][2]int16{{0, 0}, {10, 10}, {31, 20}} {
		want := sp.At(p[0], p[1])
		for dy := range 4 {
			for dx := range 4 {
				r, _, _, _ := img.At(int(p[0])*4+dx, int(p[1])*4+dy).RGBA()
				if (r>>8 < 128) != want {
					t.Fatalf("bloco de (%d,%d) não é uniforme", p[0], p[1])
				}
			}
		}
	}
}

// Ida e volta: exportar e reimportar tem que devolver a MESMA arte. Sem isso,
// editar um bicho no Piskel e trazer de volta perderia pixel.
func TestIdaEVoltaPreservaAArte(t *testing.T) {
	for _, st := range []sim.Stage{sim.StageOvo, sim.StageFilhote, sim.StageJovem, sim.StageAdulto, sim.StageVeterano} {
		sp := ui.SpriteDoEstagio(st)
		var b bytes.Buffer
		if err := escrever(&b, sp, 1); err != nil {
			t.Fatal(err)
		}
		volta, err := importar(b.Bytes(), int(sp.W), int(sp.H))
		if err != nil {
			t.Fatal(err)
		}
		for y := int16(0); y < sp.H; y++ {
			for x := int16(0); x < sp.W; x++ {
				if volta[y][x] != sp.At(x, y) {
					t.Fatalf("estágio %v: (%d,%d) mudou na ida e volta", st, x, y)
				}
			}
		}
	}
}

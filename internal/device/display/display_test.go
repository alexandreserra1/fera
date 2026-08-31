package display

import (
	"strings"
	"testing"
)

// arte converte ASCII pra Sprite. Existe só no teste: em produção o sprite já
// nasce empacotado na flash.
func arte(linhas ...string) Sprite {
	h := int16(len(linhas))
	w := int16(len(linhas[0]))
	stride := (w + 7) / 8
	bits := make([]byte, int(stride)*int(h))
	for y, l := range linhas {
		for x, c := range l {
			if c == '#' {
				bits[y*int(stride)+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return Sprite{W: w, H: h, Stride: stride, Bits: bits}
}

func desenho(s string) string { return strings.TrimSpace(s) }

func TestSetEGetIdaEVolta(t *testing.T) {
	b := NewBuffer(16, 8)
	for _, p := range [][2]int16{{0, 0}, {15, 7}, {7, 3}, {8, 4}} {
		b.Set(p[0], p[1], true)
		if !b.Get(p[0], p[1]) {
			t.Errorf("(%d,%d) não acendeu", p[0], p[1])
		}
		b.Set(p[0], p[1], false)
		if b.Get(p[0], p[1]) {
			t.Errorf("(%d,%d) não apagou", p[0], p[1])
		}
	}
}

// Escrever fora da tela não pode dar pânico: no device isso derruba o bicho e
// o dono vê tela branca sem nenhuma pista do motivo.
func TestForaDaTelaEhNoOpNaoPanico(t *testing.T) {
	b := NewBuffer(8, 8)
	for _, p := range [][2]int16{{-1, 0}, {0, -1}, {8, 0}, {0, 8}, {999, 999}, {-999, -999}} {
		b.Set(p[0], p[1], true)
		if b.Get(p[0], p[1]) {
			t.Errorf("(%d,%d) fora da tela virou pixel aceso", p[0], p[1])
		}
	}
	for _, v := range b.Bits {
		if v != 0 {
			t.Fatal("escrita fora da tela sujou o framebuffer")
		}
	}
}

func TestBlitDesenhaOSprite(t *testing.T) {
	d := NewFake(8, 6)
	d.Blit(2, 1, arte(
		"###",
		"#.#",
		"###",
	))

	want := desenho(`
........
..###...
..#.#...
..###...
........
........`)
	if got := desenho(d.Buf.String()); got != want {
		t.Errorf("blit errado:\n%s\n\nesperado:\n%s", got, want)
	}
}

// Zero no sprite é transparente, não branco. Se pintasse zero, todo sprite
// viraria um retângulo opaco e o bicho não teria silhueta.
func TestZeroNoSpriteEhTransparente(t *testing.T) {
	d := NewFake(8, 4)
	d.Buf.Fill(0, 0, 8, 4, true)
	d.Blit(0, 0, arte(
		"#.#.#.#.",
		"........",
	))

	for x := int16(0); x < 8; x++ {
		if !d.Buf.Get(x, 0) || !d.Buf.Get(x, 1) {
			t.Fatalf("o zero do sprite apagou o fundo em x=%d", x)
		}
	}
}

func TestBlitClipaNaBorda(t *testing.T) {
	d := NewFake(8, 4)
	d.Blit(6, 2, arte("####", "####", "####", "####")) // metade sai da tela

	want := desenho(`
........
........
......##
......##`)
	if got := desenho(d.Buf.String()); got != want {
		t.Errorf("clipping errado:\n%s\n\nesperado:\n%s", got, want)
	}
}

// A escala é o que permite guardar arte 16x16 na flash e mostrar 32x32 na
// tela: quatro vezes menos byte pra guardar e pra desenhar à mão.
func TestBlitScaledAmpliaCadaPixel(t *testing.T) {
	d := NewFake(8, 6)
	d.Buf.BlitScaled(1, 1, arte("#.", ".#"), 2)

	want := desenho(`
........
.##.....
.##.....
...##...
...##...
........`)
	if got := desenho(d.Buf.String()); got != want {
		t.Errorf("escala errada:\n%s\n\nesperado:\n%s", got, want)
	}
}

func TestRectDesenhaSoAMoldura(t *testing.T) {
	d := NewFake(8, 5)
	d.Buf.Rect(1, 1, 5, 4, true)

	want := desenho(`
........
.#####..
.#...#..
.#...#..
.#####..`)
	if got := desenho(d.Buf.String()); got != want {
		t.Errorf("moldura errada:\n%s\n\nesperado:\n%s", got, want)
	}
}

func TestClearApagaTudo(t *testing.T) {
	d := NewFake(16, 8)
	d.Buf.Fill(0, 0, 16, 8, true)
	d.Clear()
	for _, v := range d.Buf.Bits {
		if v != 0 {
			t.Fatal("Clear deixou pixel aceso")
		}
	}
}

// O orçamento de RAM do docs/06 conta com 1 KB por framebuffer no SSD1306 e
// 3 KB no Sharp. Se o layout mudar e o buffer inchar, o orçamento cai junto.
func TestTamanhoDoFramebufferBateComOOrcamento(t *testing.T) {
	for _, c := range []struct {
		nome     string
		w, h     int16
		esperado int
	}{
		{"SSD1306 128x64", 128, 64, 1024},
		{"Sharp 168x144", 168, 144, 3024},
	} {
		if got := len(NewBuffer(c.w, c.h).Bits); got != c.esperado {
			t.Errorf("%s: %d bytes, o docs/06 orça %d", c.nome, got, c.esperado)
		}
	}
}

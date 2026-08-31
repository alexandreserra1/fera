package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// pngDeArte monta um PNG a partir de ASCII, pra que o teste não dependa de
// arquivo externo.
func pngDeArte(linhas []string, aceso, apagado color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, len(linhas[0]), len(linhas)))
	for y, l := range linhas {
		for x, c := range l {
			if c == '#' {
				img.Set(x, y, aceso)
			} else {
				img.Set(x, y, apagado)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

var preto = color.RGBA{0, 0, 0, 255}
var branco = color.RGBA{255, 255, 255, 255}

func TestImportaNaMesmaResolucao(t *testing.T) {
	arte := []string{
		"####",
		"#..#",
		"#..#",
		"####",
	}
	got, err := converter(pngDeArte(arte, preto, branco), 4, 4, opcoes{limiar: 128})
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.Join(arte, "\n") {
		t.Errorf("ida e volta mudou a arte:\n%s\n\nesperado:\n%s", got, strings.Join(arte, "\n"))
	}
}

// PNG com fundo escuro e desenho claro é o caso comum de arte de device
// mono. Sem -inverter, o bicho vira o negativo dele.
func TestInverterTrocaFundoEDesenho(t *testing.T) {
	arte := []string{"#.", ".#"}
	semInverter, err := converter(pngDeArte(arte, branco, preto), 2, 2, opcoes{limiar: 128})
	if err != nil {
		t.Fatal(err)
	}
	comInverter, err := converter(pngDeArte(arte, branco, preto), 2, 2, opcoes{limiar: 128, inverter: true})
	if err != nil {
		t.Fatal(err)
	}
	if semInverter == comInverter {
		t.Fatal("-inverter não mudou nada")
	}
	if comInverter != "#.\n.#" {
		t.Errorf("com -inverter deu %q, esperado a arte original", comInverter)
	}
}

// Reduzir 1-bit por vizinho mais próximo perde traço fino: uma linha de 1px
// some se cair entre duas amostras. Voto da maioria preserva a forma.
func TestReduzPorMaioriaNaoPorAmostragem(t *testing.T) {
	// 8x8 com metade de cima cheia: ao reduzir pra 4x4, as duas linhas de
	// cima têm que ficar cheias e as de baixo vazias
	arte := []string{
		"########", "########", "########", "########",
		"........", "........", "........", "........",
	}
	got, err := converter(pngDeArte(arte, preto, branco), 4, 4, opcoes{limiar: 128})
	if err != nil {
		t.Fatal(err)
	}
	want := "####\n####\n....\n...."
	if got != want {
		t.Errorf("redução errada:\n%s\n\nesperado:\n%s", got, want)
	}
}

// Fonte não quadrada (46x49 do pack CC0 do Flipper) tem que virar 32x32 sem
// esmagar: recorta o centro do lado maior em vez de distorcer.
func TestFonteNaoQuadradaNaoDistorce(t *testing.T) {
	linhas := make([]string, 49)
	for i := range linhas {
		linhas[i] = strings.Repeat(".", 46)
	}
	// um quadrado centrado: se distorcer, deixa de ser quadrado
	for y := 20; y < 29; y++ {
		linhas[y] = strings.Repeat(".", 19) + strings.Repeat("#", 9) + strings.Repeat(".", 18)
	}
	got, err := converter(pngDeArte(linhas, preto, branco), 32, 32, opcoes{limiar: 128})
	if err != nil {
		t.Fatal(err)
	}
	saida := strings.Split(got, "\n")
	if len(saida) != 32 || len(saida[0]) != 32 {
		t.Fatalf("saída %dx%d, esperado 32x32", len(saida[0]), len(saida))
	}
	// mede o quadrado na saída: largura e altura têm que ser parecidas
	var larg, alt int
	for _, l := range saida {
		if n := strings.Count(l, "#"); n > larg {
			larg = n
		}
		if strings.Contains(l, "#") {
			alt++
		}
	}
	if d := larg - alt; d > 2 || d < -2 {
		t.Errorf("o quadrado virou %dx%d: distorceu", larg, alt)
	}
}

func TestPngInvalidoDaErro(t *testing.T) {
	if _, err := converter([]byte("nem de longe um png"), 32, 32, opcoes{limiar: 128}); err == nil {
		t.Error("entrada inválida foi aceita")
	}
}

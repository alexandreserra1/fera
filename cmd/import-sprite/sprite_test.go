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

// Pack de terceiro quase nunca vem em arquivo por sprite: vem em tilesheet,
// uma grade de células. Sem recorte de célula, o importador só serve pra
// arte que alguém já separou.
func TestRecortaCelulaDeTilesheet(t *testing.T) {
	// grade 2x2 de células 4x4, sem espaçamento; cada célula com um padrão
	linhas := []string{
		"####....",
		"####....",
		"####....",
		"####....",
		"........",
		"........",
		"..##....",
		"..##....",
	}
	png := pngDeArte(linhas, preto, branco)

	// célula (0,0) é toda cheia
	got, err := converter(png, 4, 4, opcoes{limiar: 128, tile: 4, col: 0, lin: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got != "####\n####\n####\n####" {
		t.Errorf("célula (0,0) = %q", got)
	}

	// célula (0,1) tem só o bloco de baixo
	got, err = converter(png, 4, 4, opcoes{limiar: 128, tile: 4, col: 0, lin: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != "....\n....\n..##\n..##" {
		t.Errorf("célula (0,1) = %q", got)
	}
}

// Muitos packs põem 1px entre as células. Ignorar isso desalinha a grade
// inteira a partir da segunda coluna.
func TestEspacamentoEntreCelulas(t *testing.T) {
	linhas := []string{
		"##.##",
		"##.##",
		".....",
		"##.##",
		"##.##",
	}
	png := pngDeArte(linhas, preto, branco)
	got, err := converter(png, 2, 2, opcoes{limiar: 128, tile: 2, esp: 1, col: 1, lin: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != "##\n##" {
		t.Errorf("com espaçamento, a célula (1,1) saiu %q", got)
	}
}

func TestCelulaForaDaGradeDaErro(t *testing.T) {
	png := pngDeArte([]string{"##", "##"}, preto, branco)
	if _, err := converter(png, 2, 2, opcoes{limiar: 128, tile: 2, col: 99, lin: 0}); err == nil {
		t.Error("célula fora da grade foi aceita")
	}
}

// Ampliar pixel art por repetição transforma cada pixel num bloco e o
// resultado vira Minecraft. Scale2x (EPX) olha os quatro vizinhos e suaviza
// diagonal, que é o que faz 16x16 virar 64x64 parecendo desenho.
func TestScale2xSuavizaDiagonal(t *testing.T) {
	// uma escada de 1px: por repetição vira degrau quadrado; com Scale2x, os
	// cantos internos são preenchidos.
	entrada := [][]bool{
		{true, false, false},
		{true, true, false},
		{true, true, true},
	}
	got := scale2x(entrada)

	if len(got) != 6 || len(got[0]) != 6 {
		t.Fatalf("saída %dx%d, esperado 6x6", len(got[0]), len(got))
	}
	// o canto da escada tem que ganhar pixel, senão nada foi suavizado
	porRepeticao := 0
	for y := range got {
		for x := range got[y] {
			if got[y][x] {
				porRepeticao++
			}
		}
	}
	// repetição pura daria exatamente 4x a massa original (6 pixels * 4 = 24)
	if porRepeticao == 24 {
		t.Error("Scale2x devolveu o mesmo que repetição: nada foi suavizado")
	}
}

// Área sólida não pode ganhar buraco nem borda serrilhada: Scale2x só age
// onde há diagonal.
func TestScale2xNaoMexeEmAreaSolida(t *testing.T) {
	entrada := [][]bool{
		{true, true, true},
		{true, true, true},
		{true, true, true},
	}
	got := scale2x(entrada)
	for y := range got {
		for x := range got[y] {
			if !got[y][x] {
				t.Fatalf("bloco sólido ganhou buraco em (%d,%d)", x, y)
			}
		}
	}
}

func TestScale2xDobraAsDimensoes(t *testing.T) {
	got := scale2x(make([][]bool, 5))
	if len(got) != 10 {
		t.Errorf("altura %d, esperado 10", len(got))
	}
}

// Ampliar 4x é Scale2x duas vezes: 16x16 vira 64x64, que é o tamanho do
// bicho na tela.
func TestAmpliacaoDe16Para64UsaScale2x(t *testing.T) {
	arte := []string{
		"................", "......####......", ".....######.....", "....##....##....",
		"....##....##....", "....########....", "....##....##....", "....##....##....",
		"................", "................", "................", "................",
		"................", "................", "................", "................",
	}
	png := pngDeArte(arte, preto, branco)

	blocado, err := converter(png, 64, 64, opcoes{limiar: 128})
	if err != nil {
		t.Fatal(err)
	}
	suave, err := converter(png, 64, 64, opcoes{limiar: 128, suavizar: true})
	if err != nil {
		t.Fatal(err)
	}
	if blocado == suave {
		t.Error("-suavizar não mudou nada")
	}
}

// Package ui desenha o bicho. É puro: recebe uma sim.View e um Display, e não
// conhece pino, barramento nem relógio.
//
// Por isso todo teste de tela deste projeto roda com `go test` no Mac,
// comparando framebuffer, e não com println na serial de uma placa ligada.
package ui

import (
	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/sim"
)

// Layout de 128x64: o bicho toma a metade esquerda inteira, os quatro
// atributos ocupam a direita.
//
// A primeira versão dava 32x32 ao bicho — 12% da tela — e espremia quatro
// barras com rótulo e dois textos em volta. O bicho não estava só feio, estava
// pequeno demais pra ter desenho. A referência é o golfinho do Flipper Zero,
// que roda na mesma tela de 128x64 e dá quase tudo pra criatura.
//
// Quatro atributos é o teto do que cabe e do que um humano equilibra, que é
// exatamente o argumento do sim-core pra não existir um quinto: a tela é a
// restrição de design, não uma consequência dela.
const (
	Largura = 128
	Altura  = 64

	escalaBicho = 1 // arte nativa em 64x64; ver internal/device/ui/sprites.go
	bichoX      = 0
	bichoY      = 0

	rotuloX   = 66
	barraX    = 80
	barraW    = 46
	barraH    = 5
	primeiraY = 3
	passoY    = 12

	// O nome do estágio e do traço vão embaixo das barras, do lado direito.
	textoX   = 66
	estagioY = 51
	tracoY   = 58
)

// Buffer de formatação, alocado uma vez. Nenhum make dentro do Render: em
// regime o laço do device não pode alocar, senão o GC conservativo do TinyGo
// acorda e o consumo médio deixa de fechar. TestRenderNaoAloca trava isso.
var numBuf [8]byte

// QuadrosDeRespiracao é o tamanho do ciclo de animação ociosa.
//
// Oito quadros a 10 fps dão um ciclo de 0,8 s, que é ritmo de respiração
// calma. Menos quadros ficam mecânicos; mais não se percebe.
const QuadrosDeRespiracao = 8

// respiracao devolve o deslocamento vertical do bicho no quadro dado.
//
// Um sprite deslocado alguns pixels em ciclo é como jogo retrô faz animação
// ociosa desde sempre: custa ZERO arte nova e transforma uma figura colada na
// tela num bicho que parece vivo.
//
// A curva sobe devagar e desce devagar (0,1,2,2,2,1,0,0), em vez de alternar
// entre dois valores: alternância pura lê como tremor, não como respiração.
func respiracao(quadro int) int16 {
	desloc := [QuadrosDeRespiracao]int16{0, 1, 2, 2, 2, 1, 0, 0}
	q := quadro % QuadrosDeRespiracao
	if q < 0 {
		q += QuadrosDeRespiracao
	}
	return desloc[q]
}

// Render desenha o bicho parado. É o que as telas douradas travam e o que o
// device mostra quando ninguém está mexendo nele.
func Render(b *display.Buffer, v sim.View) { RenderQuadro(b, v, 0) }

// RenderQuadro desenha UM frame no framebuffer. Buffer entra, pixels saem:
// nenhuma interface, nenhum driver, nenhum relógio. É essa assinatura que faz todo
// teste de tela do projeto rodar no Mac.
//
// Não faz laço e não dorme. Com memory LCD a imagem fica na tela sem energia,
// então quem decide QUANDO redesenhar é o loop principal, não o renderer.
func RenderQuadro(b *display.Buffer, v sim.View, quadro int) {
	b.Clear()
	b.BlitScaled(bichoX, bichoY+respiracao(quadro), SpriteDoEstagio(v.Stage), escalaBicho)

	DrawText(b, textoX, estagioY, NomeDoEstagio(v.Stage))
	DrawText(b, textoX, tracoY, NomeDoTraco(v.Trait))

	atributos := [4]struct {
		rotulo string
		valor  int32
	}{
		{"VIG", v.Stats.Vigor},
		{"ANI", v.Stats.Animo},
		{"SAU", v.Stats.Saude},
		{"VIN", v.Stats.Vinculo},
	}
	// Rótulo e barra, sem o número. Com 62 px de largura não cabem os três, e
	// o número era precisão redundante: a barra tem 46 px pra 100 pontos, ou
	// seja, cada pixel já vale ~2 pontos.
	for i, a := range atributos {
		y := primeiraY + int16(i)*passoY
		DrawText(b, rotuloX, y, a.rotulo)
		barra(b, barraX, y, a.valor)
	}
}

// barra desenha moldura vazia e preenche a fração. Moldura sempre visível
// porque atributo zerado precisa ser diferente de atributo ausente: bicho
// morrendo tem que parecer bicho morrendo, não tela quebrada.
func barra(b *display.Buffer, x, y int16, valor int32) {
	b.Rect(x, y-1, barraW, barraH+2, true)

	if valor < 0 {
		valor = 0
	}
	if valor > sim.Max {
		valor = sim.Max
	}
	cheio := int16(int32(barraW-4) * valor / int32(sim.Max))
	if cheio > 0 {
		b.Fill(x+2, y+1, cheio, barraH-2, true)
	}
}

var nomesEstagio = [...]string{"OVO", "FILHOTE", "JOVEM", "ADULTO", "VETERANO"}

func NomeDoEstagio(s sim.Stage) string {
	if int(s) >= len(nomesEstagio) {
		return "OVO"
	}
	return nomesEstagio[s]
}

var nomesTraco = [...]string{"NEUTRO", "TEIMOSO", "AGITADO", "CALMO", "FERINO"}

func NomeDoTraco(t sim.Trait) string {
	if int(t) >= len(nomesTraco) {
		return "NEUTRO"
	}
	return nomesTraco[t]
}

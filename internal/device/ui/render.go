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

// Layout de 128x64. Bicho grande à esquerda, os quatro atributos à direita.
//
// Quatro é o teto do que cabe aqui e do que um humano equilibra, que é
// exatamente o argumento do sim-core pra não existir um quinto atributo: a
// tela é a restrição de design, não uma consequência dela.
const (
	Largura = 128
	Altura  = 64

	escalaBicho = 1 // arte nativa em 32x32; ver internal/device/ui/sprites.go
	bichoX      = 4
	bichoY      = 14

	rotuloX   = 40
	valorX    = 54
	barraX    = 70
	barraW    = 54
	barraH    = 5
	primeiraY = 8
	passoY    = 12
)

// Buffer de formatação, alocado uma vez. Nenhum make dentro do Render: em
// regime o laço do device não pode alocar, senão o GC conservativo do TinyGo
// acorda e o consumo médio deixa de fechar. TestRenderNaoAloca trava isso.
var numBuf [8]byte

// Render desenha UM frame no framebuffer. Buffer entra, pixels saem: nenhuma
// interface, nenhum driver, nenhum relógio. É essa assinatura que faz todo
// teste de tela do projeto rodar no Mac.
//
// Não faz laço e não dorme. Com memory LCD a imagem fica na tela sem energia,
// então quem decide QUANDO redesenhar é o loop principal, não o renderer.
func Render(b *display.Buffer, v sim.View) {
	b.Clear()
	b.BlitScaled(bichoX, bichoY, SpriteDoEstagio(v.Stage), escalaBicho)

	DrawText(b, bichoX, 2, NomeDoEstagio(v.Stage))
	DrawText(b, bichoX, Altura-7, NomeDoTraco(v.Trait))

	atributos := [4]struct {
		rotulo string
		valor  int32
	}{
		{"VIG", v.Stats.Vigor},
		{"ANI", v.Stats.Animo},
		{"SAU", v.Stats.Saude},
		{"VIN", v.Stats.Vinculo},
	}
	for i, a := range atributos {
		y := primeiraY + int16(i)*passoY
		DrawText(b, rotuloX, y, a.rotulo)
		DrawBytes(b, valorX, y, itoa(numBuf[:0], int32(sim.Pct(a.valor))))
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

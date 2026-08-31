package ui

import "github.com/ale/fera/internal/device/display"

// Fonte 3x5. Pequena de propósito: com 3 px de largura cabem 3 rótulos de
// atributo e o nome do estágio numa tela de 128 px sem espremer o bicho.
//
// A arte fica em const string (flash) e é empacotada no init. Guardar hex à
// mão seria menos RAM e mais bug: fonte com typo você só descobre olhando, e
// olhando errado. O custo real está medido em TestOrcamentoDeRAMDaUI.
const arteFonte = `
A .#. #.# ### #.# #.#
B ##. #.# ##. #.# ##.
C .## #.. #.. #.. .##
D ##. #.# #.# #.# ##.
E ### #.. ##. #.. ###
F ### #.. ##. #.. #..
G .## #.. #.# #.# .##
H #.# #.# ### #.# #.#
I ### .#. .#. .#. ###
J ..# ..# ..# #.# .#.
K #.# #.# ##. #.# #.#
L #.. #.. #.. #.. ###
M #.# ### ### #.# #.#
N #.# ##. ### .## #.#
O .#. #.# #.# #.# .#.
P ##. #.# ##. #.. #..
Q .#. #.# #.# ##. .##
R ##. #.# ##. #.# #.#
S .## #.. .#. ..# ##.
T ### .#. .#. .#. .#.
U #.# #.# #.# #.# ###
V #.# #.# #.# .#. .#.
W #.# #.# ### ### #.#
X #.# #.# .#. #.# #.#
Y #.# #.# .#. .#. .#.
Z ### ..# .#. #.. ###
0 ### #.# #.# #.# ###
1 .#. ##. .#. .#. ###
2 ##. ..# .#. #.. ###
3 ##. ..# .#. ..# ##.
4 #.# #.# ### ..# ..#
5 ### #.. ##. ..# ##.
6 .## #.. ### #.# ###
7 ### ..# .#. #.. #..
8 ### #.# ### #.# ###
9 ### #.# ### ..# ##.
- ... ... ### ... ...
% #.# ..# .#. #.. #.#
`

const (
	larguraGlifo = 3
	alturaGlifo  = 5
	avancoGlifo  = larguraGlifo + 1 // 1 px de respiro entre caracteres
)

// glifos é indexado por byte do caractere. 256 entradas de 5 bytes = 1280 B,
// e cada byte guarda uma LINHA nos 3 bits altos, no mesmo sentido do Buffer.
var glifos [256][alturaGlifo]byte

func init() {
	for i := 0; i < len(arteFonte); {
		// pula quebras de linha e espaços de indentação
		for i < len(arteFonte) && (arteFonte[i] == '\n' || arteFonte[i] == '\r') {
			i++
		}
		if i+2+alturaGlifo*(larguraGlifo+1) > len(arteFonte) {
			break
		}
		c := arteFonte[i]
		p := i + 2 // pula o caractere e o espaço
		for linha := 0; linha < alturaGlifo; linha++ {
			var b byte
			for col := 0; col < larguraGlifo; col++ {
				if arteFonte[p+col] == '#' {
					b |= 0x80 >> col
				}
			}
			glifos[c][linha] = b
			p += larguraGlifo + 1
		}
		i = p
	}
	glifos[' '] = [alturaGlifo]byte{} // espaço é explicitamente vazio
}

// DrawText escreve texto no buffer. Só maiúsculas, dígitos, espaço, '-' e '%':
// TestTodoCaractereRenderizadoTemGlifo garante que nada que o renderer usa
// caia fora disso e apareça como buraco na tela.
func DrawText(b *display.Buffer, x, y int16, s string) int16 {
	for i := 0; i < len(s); i++ {
		x = glifo(b, x, y, s[i])
	}
	return x
}

// DrawBytes existe pra desenhar número sem passar por string. Converter
// []byte pra string alocaria, e alocar no laço de render é o que acorda o GC
// do TinyGo num device que deveria estar dormindo.
func DrawBytes(b *display.Buffer, x, y int16, p []byte) int16 {
	for _, c := range p {
		x = glifo(b, x, y, c)
	}
	return x
}

func glifo(b *display.Buffer, x, y int16, c byte) int16 {
	g := glifos[c]
	for linha := int16(0); linha < alturaGlifo; linha++ {
		for col := int16(0); col < larguraGlifo; col++ {
			if g[linha]&(0x80>>col) != 0 {
				b.Set(x+col, y+linha, true)
			}
		}
	}
	return x + avancoGlifo
}

// itoa escreve o número num buffer fixo, sem alocar e sem fmt.
//
// fmt puxa reflection e explode o binário no TinyGo. São 15 linhas pra evitar
// dezenas de KB de flash: nesta camada isso é a troca certa.
func itoa(dst []byte, n int32) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	if n < 0 {
		dst = append(dst, '-')
		n = -n
	}
	var tmp [10]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(dst, tmp[i:]...)
}

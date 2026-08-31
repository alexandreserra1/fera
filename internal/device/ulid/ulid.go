// Package ulid gera os identificadores de evento do device.
//
// ULID e não UUID por uma razão estrutural: o sim.isAfter desempata dois
// eventos do mesmo segundo comparando os IDs como texto. Isso só é correto se
// ID mais novo for lexicograficamente maior, e é exatamente o que o ULID
// garante ao pôr o timestamp nos bits mais significativos. Com UUID v4 o
// desempate seria sorteio, e device e servidor aplicariam o lote em ordens
// diferentes.
//
// 128 bits: 48 de timestamp em milissegundos, 80 de aleatoriedade.
package ulid

import "time"

// Crockford base32: sem I, L, O e U, pra não confundir com 1, 0 e nem formar
// palavra sem querer. 32 símbolos = 5 bits por caractere, e 26 caracteres
// cobrem os 128 bits com 2 bits de sobra no primeiro.
const alfabeto = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Tamanho é o comprimento em caracteres. O store reserva exatamente isto por
// campo de ID no registro da fila.
const Tamanho = 26

var reverso = func() [256]int8 {
	var r [256]int8
	for i := range r {
		r[i] = -1
	}
	for i := 0; i < len(alfabeto); i++ {
		r[alfabeto[i]] = int8(i)
	}
	return r
}()

// New monta o ULID a partir do instante e de 80 bits de entropia já
// sorteados.
//
// A entropia entra como ARRAY POR VALOR, não como callback: passar um slice
// pra uma função-parâmetro faz o buffer escapar pro heap, e isso é uma
// alocação por evento num device que não deveria acordar o GC. Quem chama
// sorteia (RNG de hardware no device, fonte fixa no teste) e este pacote só
// codifica.
func New(agora time.Time, entropia [10]byte) string {
	var b [16]byte

	ms := agora.UnixMilli()
	if ms < 0 {
		ms = 0
	}
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	copy(b[6:], entropia[:])

	var out [Tamanho]byte
	codifica(&out, &b)
	return string(out[:])
}

// codifica escreve os 128 bits em 26 caracteres de 5 bits, do mais
// significativo pro menos. O primeiro caractere carrega só 3 bits úteis
// (26*5 = 130, sobram 2), e é por isso que ULID válido nunca começa depois
// de '7'.
func codifica(out *[Tamanho]byte, b *[16]byte) {
	for i := 0; i < Tamanho; i++ {
		bit := i * 5
		var v uint16
		for j := 0; j < 5; j++ {
			pos := bit + j
			if pos < 2 {
				continue // os 2 bits de padding no topo
			}
			pos -= 2
			if b[pos/8]&(0x80>>(pos%8)) != 0 {
				v |= 1 << (4 - j)
			}
		}
		out[i] = alfabeto[v&0x1F]
	}
}

// Instante lê o timestamp de volta. Serve pra ordenar e pra detectar relógio
// mentiroso depois de um reboot a frio.
func Instante(id string) (time.Time, bool) {
	if len(id) != Tamanho {
		return time.Time{}, false
	}
	var ms int64
	// os 10 primeiros caracteres cobrem os 48 bits de timestamp (10*5 = 50,
	// com os mesmos 2 bits de padding no topo)
	for i := 0; i < 10; i++ {
		v := reverso[id[i]]
		if v < 0 {
			return time.Time{}, false
		}
		ms = ms<<5 | int64(v)
	}
	for i := 10; i < Tamanho; i++ {
		if reverso[id[i]] < 0 {
			return time.Time{}, false
		}
	}
	return time.UnixMilli(ms).UTC(), true
}

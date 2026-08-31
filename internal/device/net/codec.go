package net

import (
	"time"

	"github.com/ale/fera/internal/sim"
)

// Serialização escrita à mão, sem encoding/json.
//
// encoding/json usa reflection, e reflection no TinyGo custa binário e RAM
// num device que tem que caber e dormir. O skill firmware é explícito: sem
// json no device, converte na hora do envio. São 60 linhas pra economizar
// dezenas de KB, e o formato é fixo e testado contra o handler de verdade.

// codificaLote escreve {"events":[...]} no formato do api-contract.
// Anexa em dst pra que o chamador reaproveite o buffer e o laço não aloque.
func codificaLote(dst []byte, evs []sim.Event) []byte {
	dst = append(dst, `{"events":[`...)
	for i, ev := range evs {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, `{"id":`...)
		dst = texto(dst, ev.ID)
		dst = append(dst, `,"kind":`...)
		dst = texto(dst, sim.KindName(ev.Kind))
		dst = append(dst, `,"at":"`...)
		dst = escreveRFC3339(dst, ev.At)
		dst = append(dst, `","payload":{`...)

		primeiro := true
		if ev.Kcal != 0 {
			dst, primeiro = campo(dst, primeiro, "kcal", int64(ev.Kcal))
		}
		if ev.Zone != 0 {
			dst, primeiro = campo(dst, primeiro, "zone", int64(ev.Zone))
		}
		if ev.Minutes != 0 {
			dst, primeiro = campo(dst, primeiro, "minutes", int64(ev.Minutes))
		}
		if ev.PeerID != "" {
			if !primeiro {
				dst = append(dst, ',')
			}
			dst = append(dst, `"peer_id":`...)
			dst = texto(dst, ev.PeerID)
		}
		dst = append(dst, '}', '}')
	}
	return append(dst, ']', '}')
}

func campo(dst []byte, primeiro bool, nome string, v int64) ([]byte, bool) {
	if !primeiro {
		dst = append(dst, ',')
	}
	dst = append(dst, '"')
	dst = append(dst, nome...)
	dst = append(dst, `":`...)
	dst = numero(dst, v)
	return dst, false
}

// texto escreve uma string JSON com escape. ULID não tem caractere especial,
// mas o ID chega do store e do BLE, e confiar em entrada é como se produz
// JSON quebrado que o servidor rejeita sem explicar.
func texto(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0',
				hex[c>>4], hex[c&0x0F])
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"')
}

const hex = "0123456789abcdef"

func numero(dst []byte, n int64) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	if n < 0 {
		dst = append(dst, '-')
		n = -n
	}
	var tmp [20]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(dst, tmp[i:]...)
}

// escreveRFC3339 escreve o instante em UTC, sem passar por time.Format.
// TestFormatoDeTempoBateComOTimeFormat compara os dois: se divergirem, o
// servidor recebe timestamp errado e o fold sai fora de ordem.
func escreveRFC3339(dst []byte, t time.Time) []byte {
	t = t.UTC()
	a, m, d := t.Date()
	h, min, s := t.Clock()
	dst = doisOuMais(dst, a, 4)
	dst = append(dst, '-')
	dst = doisOuMais(dst, int(m), 2)
	dst = append(dst, '-')
	dst = doisOuMais(dst, d, 2)
	dst = append(dst, 'T')
	dst = doisOuMais(dst, h, 2)
	dst = append(dst, ':')
	dst = doisOuMais(dst, min, 2)
	dst = append(dst, ':')
	dst = doisOuMais(dst, s, 2)
	return append(dst, 'Z')
}

func doisOuMais(dst []byte, v, largura int) []byte {
	var tmp [8]byte
	i := len(tmp)
	for v > 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	for len(tmp)-i < largura {
		i--
		tmp[i] = '0'
	}
	return append(dst, tmp[i:]...)
}

// campoInt lê um inteiro de um JSON plano procurando "nome":.
//
// Não é um parser: só serve pra resposta do ingest, que é um objeto raso de
// quatro inteiros. Objeto aninhado com campo de mesmo nome enganaria isto, e
// é por isso que ele não sai deste pacote.
func campoInt(b []byte, nome string) (int64, bool) {
	alvo := make([]byte, 0, len(nome)+3)
	alvo = append(alvo, '"')
	alvo = append(alvo, nome...)
	alvo = append(alvo, '"')

	i := indice(b, alvo)
	if i < 0 {
		return 0, false
	}
	i += len(alvo)
	for i < len(b) && (b[i] == ' ' || b[i] == ':') {
		i++
	}
	neg := false
	if i < len(b) && b[i] == '-' {
		neg = true
		i++
	}
	if i >= len(b) || b[i] < '0' || b[i] > '9' {
		return 0, false
	}
	var v int64
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		v = v*10 + int64(b[i]-'0')
		i++
	}
	if neg {
		v = -v
	}
	return v, true
}

func indice(b, alvo []byte) int {
	for i := 0; i+len(alvo) <= len(b); i++ {
		igual := true
		for j := range alvo {
			if b[i+j] != alvo[j] {
				igual = false
				break
			}
		}
		if igual {
			return i
		}
	}
	return -1
}

// campoTexto lê uma string de um JSON plano, com o mesmo escopo restrito do
// campoInt: serve pra resposta do register, que é um objeto raso de três
// strings. Trata escape de aspas e barra, que é o que o servidor pode emitir.
func campoTexto(b []byte, nome string) (string, bool) {
	alvo := make([]byte, 0, len(nome)+3)
	alvo = append(alvo, '"')
	alvo = append(alvo, nome...)
	alvo = append(alvo, '"')

	i := indice(b, alvo)
	if i < 0 {
		return "", false
	}
	i += len(alvo)
	for i < len(b) && (b[i] == ' ' || b[i] == ':') {
		i++
	}
	if i >= len(b) || b[i] != '"' {
		return "", false
	}
	i++

	var out []byte
	for i < len(b) {
		switch b[i] {
		case '"':
			return string(out), true
		case '\\':
			i++
			if i >= len(b) {
				return "", false
			}
			switch b[i] {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			default:
				out = append(out, b[i])
			}
		default:
			out = append(out, b[i])
		}
		i++
	}
	return "", false
}

package sig

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

func TestAssinaturaEhDeterministica(t *testing.T) {
	k := Chave("token-do-device")
	corpo := []byte(`{"events":[]}`)

	a := Assinar(k, "POST", "/v1/pets/abc/events", t0, corpo)
	b := Assinar(k, "POST", "/v1/pets/abc/events", t0, corpo)
	if a != b {
		t.Errorf("mesma entrada deu assinaturas diferentes:\n %s\n %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("assinatura com %d caracteres, esperado 64 (BLAKE2s-256 em hex)", len(a))
	}
}

// Cada parte da requisição tem que entrar na assinatura. Se alguma ficar de
// fora, um atacante pode trocá-la sem invalidar o MAC: trocar o método, o pet
// do caminho, o instante ou o corpo são quatro ataques diferentes.
func TestTodaParteDaRequisicaoEntraNaAssinatura(t *testing.T) {
	k := Chave("token")
	base := Assinar(k, "POST", "/v1/pets/abc/events", t0, []byte("x"))

	casos := []struct {
		nome string
		got  string
	}{
		{"método", Assinar(k, "GET", "/v1/pets/abc/events", t0, []byte("x"))},
		{"caminho", Assinar(k, "POST", "/v1/pets/OUTRO/events", t0, []byte("x"))},
		{"instante", Assinar(k, "POST", "/v1/pets/abc/events", t0.Add(time.Second), []byte("x"))},
		{"corpo", Assinar(k, "POST", "/v1/pets/abc/events", t0, []byte("y"))},
		{"chave", Assinar(Chave("outro-token"), "POST", "/v1/pets/abc/events", t0, []byte("x"))},
	}
	for _, c := range casos {
		if c.got == base {
			t.Errorf("trocar %s não mudou a assinatura: dá pra adulterar sem ser notado", c.nome)
		}
	}
}

// Corpo vazio e corpo ausente têm que assinar igual: um GET não tem corpo, e
// http.Request entrega ora nil ora slice vazio.
func TestCorpoVazioENilAssinamIgual(t *testing.T) {
	k := Chave("token")
	if Assinar(k, "GET", "/x", t0, nil) != Assinar(k, "GET", "/x", t0, []byte{}) {
		t.Error("nil e slice vazio deram assinaturas diferentes")
	}
}

func TestConfereAceitaAValidaERecusaAsOutras(t *testing.T) {
	k := Chave("token")
	corpo := []byte("corpo")
	boa := Assinar(k, "POST", "/v1/x", t0, corpo)

	if !Confere(k, "POST", "/v1/x", t0, corpo, boa) {
		t.Error("a assinatura válida foi recusada")
	}
	for _, ruim := range []string{"", "abc", strings.Repeat("0", 64), boa[:63] + "0"} {
		if Confere(k, "POST", "/v1/x", t0, corpo, ruim) {
			t.Errorf("a assinatura %q foi aceita", ruim)
		}
	}
}

// A comparação tem que ser em tempo constante. Comparação byte a byte com
// saída antecipada vaza, por tempo, quantos caracteres do prefixo estão
// certos, e isso permite descobrir a assinatura tentativa por tentativa.
func TestConfereUsaComparacaoDeTempoConstante(t *testing.T) {
	// Não dá pra medir tempo de forma confiável em teste. O que dá é garantir
	// que assinaturas erradas em posições MUITO diferentes são todas
	// recusadas, e deixar a exigência registrada no código.
	k := Chave("token")
	boa := Assinar(k, "POST", "/x", t0, nil)
	errada := []byte(boa)
	for _, pos := range []int{0, 1, 31, 63} {
		e := append([]byte{}, errada...)
		if e[pos] == 'a' {
			e[pos] = 'b'
		} else {
			e[pos] = 'a'
		}
		if Confere(k, "POST", "/x", t0, nil, string(e)) {
			t.Errorf("assinatura com o byte %d trocado foi aceita", pos)
		}
	}
}

// A chave derivada não pode ser o token: se fosse, um dump do banco entregaria
// o token em claro, que é o que o device usa pra tudo.
func TestChaveDerivadaNaoContemOToken(t *testing.T) {
	tok := "qQi4sCpgILvVnHqZ8dKm3xPfTgRwYbNcJhLoAeUiSvE"
	k := Chave(tok)
	if strings.Contains(string(k[:]), tok) {
		t.Error("a chave derivada contém o token em claro")
	}
	if len(k) != 32 {
		t.Errorf("chave com %d bytes, esperado 32", len(k))
	}
	if k == Chave(tok+"x") {
		t.Error("tokens diferentes derivaram a mesma chave")
	}
}

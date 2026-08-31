package ulid

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

var entropiaFixa = [10]byte{0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42, 0x42}

func TestTem26CaracteresDeCrockford(t *testing.T) {
	id := New(t0, entropiaFixa)
	if len(id) != 26 {
		t.Fatalf("ULID com %d caracteres, esperado 26: %q", len(id), id)
	}
	for i := 0; i < len(id); i++ {
		if !strings.ContainsRune(alfabeto, rune(id[i])) {
			t.Errorf("caractere %q fora do alfabeto Crockford", id[i])
		}
	}
}

// A propriedade que o sim DEPENDE: sim.isAfter desempata dois eventos do mesmo
// segundo por ev.ID > s.LastID. Isso só está certo se ULID mais novo for
// lexicograficamente maior. Se esta ordem quebrar, o Fold aplica evento na
// ordem errada e device e servidor divergem.
func TestOrdemLexicograficaBateComOrdemDoTempo(t *testing.T) {
	var anterior string
	for i := range 500 {
		id := New(t0.Add(time.Duration(i)*time.Millisecond), entropiaFixa)
		if anterior != "" && id <= anterior {
			t.Fatalf("ULID de t+%dms (%s) não é maior que o anterior (%s)", i, id, anterior)
		}
		anterior = id
	}
}

// Mesmo instante, aleatoriedade diferente: os IDs precisam diferir, senão dois
// eventos simultâneos colidem e a idempotência descarta um deles como se fosse
// duplicata.
func TestMesmoInstanteComAleatorioDiferenteNaoColide(t *testing.T) {
	vistos := map[string]bool{}
	// dois bytes de contador: com um só, a entropia dá a volta em 256 e o
	// teste acusaria colisão do GERADOR do teste, não do ULID
	for n := range 1000 {
		var e [10]byte
		e[0] = byte(n >> 8)
		e[1] = byte(n)
		id := New(t0, e)
		if vistos[id] {
			t.Fatalf("colisão em %s na iteração %d", id, n)
		}
		vistos[id] = true
	}
}

// O timestamp tem que sobreviver ida e volta: é ele que dá ordenação estável
// mesmo quando o relógio do device mente depois de um reboot.
func TestTimestampSobreviveIdaEVolta(t *testing.T) {
	for _, at := range []time.Time{
		t0,
		time.UnixMilli(0).UTC(),
		t0.Add(50 * 365 * 24 * time.Hour),
	} {
		id := New(at, entropiaFixa)
		got, ok := Instante(id)
		if !ok {
			t.Fatalf("não consegui ler o instante de %s", id)
		}
		if got.UnixMilli() != at.UnixMilli() {
			t.Errorf("instante = %v, esperado %v (id %s)", got, at, id)
		}
	}
}

func TestIDInvalidoNaoDaPanico(t *testing.T) {
	for _, ruim := range []string{"", "curto", strings.Repeat("U", 26), strings.Repeat("!", 26)} {
		if _, ok := Instante(ruim); ok {
			t.Errorf("%q foi aceito como ULID", ruim)
		}
	}
}

// O device gera ULID a cada botão pressionado. Alocar ali acorda o GC.
func TestNewAlocaSoAString(t *testing.T) {
	// a string devolvida é uma alocação inevitável; o resto não pode alocar
	if n := testing.AllocsPerRun(200, func() { _ = New(t0, entropiaFixa) }); n > 1 {
		t.Errorf("New alocou %v vezes, esperado no máximo 1 (a string)", n)
	}
}

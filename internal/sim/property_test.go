package sim

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// genEvents produz lotes arbitrários mas plausíveis: IDs únicos e crescentes
// como um ULID de verdade, timestamps espalhados numa janela de ~1 ano.
func genEvents(t *rapid.T) []Event {
	n := rapid.IntRange(0, 40).Draw(t, "n")
	evs := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		off := rapid.Int64Range(0, 365*24*3600).Draw(t, "off")
		evs = append(evs, Event{
			ID:      string(rune('A'+i/26)) + string(rune('A'+i%26)),
			At:      t0.Add(time.Duration(off) * time.Second),
			Kind:    Kind(rapid.IntRange(int(KindEffort), int(KindEncounter)).Draw(t, "kind")),
			Kcal:    uint16(rapid.IntRange(0, 3000).Draw(t, "kcal")),
			Zone:    uint8(rapid.IntRange(1, 5).Draw(t, "zone")),
			Minutes: uint16(rapid.IntRange(0, 900).Draw(t, "min")),
			PeerID:  "p2",
		})
	}
	return evs
}

func shuffle(t *rapid.T, evs []Event) []Event {
	out := make([]Event, len(evs))
	copy(out, evs)
	for i := len(out) - 1; i > 0; i-- {
		j := rapid.IntRange(0, i).Draw(t, "swap")
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// A propriedade que sustenta o protocolo de sync inteiro: o lote pode chegar
// em qualquer ordem, inclusive dividido, e o estado final é o mesmo.
func TestPropertyFoldEhComutativo(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := genEvents(t)
		want := Fold(Genesis("pet1", t0), evs, tn)
		got := Fold(Genesis("pet1", t0), shuffle(t, evs), tn)
		if got != want {
			t.Fatalf("ordem mudou o resultado\n got: %+v\nwant: %+v", got, want)
		}
	})
}

// Retry reenvia o lote inteiro. Reenviar N vezes tem que ser no-op.
func TestPropertyFoldEhIdempotente(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := genEvents(t)
		vezes := rapid.IntRange(2, 4).Draw(t, "vezes")

		want := Fold(Genesis("pet1", t0), evs, tn)

		repetido := make([]Event, 0, len(evs)*vezes)
		for i := 0; i < vezes; i++ {
			repetido = append(repetido, evs...)
		}
		if got := Fold(Genesis("pet1", t0), repetido, tn); got != want {
			t.Fatalf("lote repetido %dx mudou o estado\n got: %+v\nwant: %+v", vezes, got, want)
		}
	})
}

// Snapshot é só uma otimização: foldar em k pedaços tem que dar o mesmo que
// foldar tudo de uma vez. Se quebrar, snapshot e replay divergem em produção.
func TestPropertyIncrementalBateComReplay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := genEvents(t)
		want := Fold(Genesis("pet1", t0), evs, tn)

		// os pedaços saem da ordem cronológica, que é como o device empurra
		ordenado := make([]Event, len(evs))
		copy(ordenado, evs)
		sortEvents(ordenado)

		s := Genesis("pet1", t0)
		i := 0
		for i < len(ordenado) {
			passo := rapid.IntRange(1, 5).Draw(t, "passo")
			if i+passo > len(ordenado) {
				passo = len(ordenado) - i
			}
			s = Fold(s, ordenado[i:i+passo], tn)
			i += passo
		}
		if s != want {
			t.Fatalf("fold em pedaços divergiu do fold inteiro\n incr: %+v\nwant: %+v", s, want)
		}
	})
}

func TestPropertyStatsFicamNosLimites(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := genEvents(t)
		off := rapid.Int64Range(0, 10*365*24*3600).Draw(t, "now")
		now := t0.Add(time.Duration(off) * time.Second)

		s := Fold(Genesis("pet1", t0), evs, tn)
		v := Project(s, now, tn)

		for _, c := range []struct {
			nome string
			v    int32
		}{
			{"fold.vigor", s.Stats.Vigor}, {"fold.animo", s.Stats.Animo},
			{"fold.saude", s.Stats.Saude}, {"fold.vinculo", s.Stats.Vinculo},
			{"view.vigor", v.Stats.Vigor}, {"view.animo", v.Stats.Animo},
			{"view.saude", v.Stats.Saude}, {"view.vinculo", v.Stats.Vinculo},
		} {
			if c.v < 0 || c.v > Max {
				t.Fatalf("%s fora de [0,%d]: %d", c.nome, Max, c.v)
			}
		}
	})
}

// Growth é o que define o estágio, e bicho não desevolui.
func TestPropertyGrowthNuncaDecresce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genEvents(t)
		b := genEvents(t)

		s1 := Fold(Genesis("pet1", t0), a, tn)
		s2 := Fold(s1, b, tn)

		if s2.Growth < s1.Growth {
			t.Fatalf("growth caiu de %d pra %d", s1.Growth, s2.Growth)
		}
		if s2.Stage < s1.Stage {
			t.Fatalf("estágio regrediu de %v pra %v", s1.Stage, s2.Stage)
		}
	})
}

// Project só aplica decaimento. Nenhum atributo pode subir com o tempo parado.
func TestPropertyProjectSoDecai(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		evs := genEvents(t)
		s := Fold(Genesis("pet1", t0), evs, tn)

		base := time.Unix(s.LastAtUnix, 0).UTC()
		d1 := rapid.Int64Range(0, 365*24*3600).Draw(t, "d1")
		d2 := rapid.Int64Range(0, 365*24*3600).Draw(t, "d2")
		if d1 > d2 {
			d1, d2 = d2, d1
		}

		perto := Project(s, base.Add(time.Duration(d1)*time.Second), tn)
		longe := Project(s, base.Add(time.Duration(d2)*time.Second), tn)

		if longe.Stats.Vigor > perto.Stats.Vigor ||
			longe.Stats.Animo > perto.Stats.Animo ||
			longe.Stats.Vinculo > perto.Stats.Vinculo {
			t.Fatalf("esperar mais aumentou atributo\n perto(+%ds): %+v\n longe(+%ds): %+v",
				d1, perto.Stats, d2, longe.Stats)
		}
	})
}

// A mutação de traço tem que ser determinística por seed e distribuída entre
// os quatro traços. Se os bits usados pra decidir e pra escolher se
// sobrepuserem, um traço sai mais que os outros e ninguém percebe sem isto.
func TestMutacaoEhUniformeEntreOsTracos(t *testing.T) {
	var conta [5]int
	const n = 200000
	for i := 0; i < n; i++ {
		seed := "evt" + itoa(i) + "pet1"
		if got := mutate(TraitNeutro, seed); got != TraitNeutro {
			conta[got]++
		}
	}
	total := conta[1] + conta[2] + conta[3] + conta[4]
	if conta[0] != 0 {
		t.Fatalf("mutate devolveu TraitNeutro %d vezes", conta[0])
	}
	esperado := total / 4
	for tr := 1; tr <= 4; tr++ {
		desvio := conta[tr] - esperado
		if desvio < 0 {
			desvio = -desvio
		}
		if desvio*100 > esperado*5 { // tolerância de 5%
			t.Errorf("traço %d saiu %d vezes, esperado ~%d (%d mutações no total)",
				tr, conta[tr], esperado, total)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Package vectors define o formato dos golden vectors e o catálogo de casos.
//
// Os vetores são o contrato entre os três runtimes do sim: servidor (Go),
// app (TinyGo/WASM) e device (TinyGo/ESP32). Os três rodam os mesmos pares
// entrada/saída. Divergência é build quebrado, não bug pra investigar depois.
//
// Este pacote vive fora de internal/sim de propósito: sim é puro e não
// conhece encoding/json. Aqui a serialização é permitida porque isto é
// ferramenta de teste, nunca caminho quente.
package vectors

import (
	"time"

	"github.com/ale/fera/internal/sim"
)

// T0 é a origem de tempo de todos os vetores. Fixa, UTC, sem surpresa.
var T0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

// Case é a entrada de um vetor: tudo que o runtime precisa pra calcular.
type Case struct {
	Name    string
	Tuning  sim.Tuning
	Initial sim.State
	Events  []sim.Event
	NowUnix int64
}

// Vector é o caso mais a saída esperada. É isto que vai pro JSON.
type Vector struct {
	Name      string      `json:"name"`
	Tuning    sim.Tuning  `json:"tuning"`
	Initial   sim.State   `json:"initial"`
	Events    []sim.Event `json:"events"`
	NowUnix   int64       `json:"now_unix"`
	Folded    sim.State   `json:"folded"`
	Projected sim.View    `json:"projected"`
}

// Compute resolve a saída esperada rodando o core de verdade. O gerador usa
// isto pra escrever o arquivo; o teste usa pra conferir contra o arquivo.
func Compute(c Case) Vector {
	folded := sim.Fold(c.Initial, c.Events, c.Tuning)
	return Vector{
		Name:      c.Name,
		Tuning:    c.Tuning,
		Initial:   c.Initial,
		Events:    c.Events,
		NowUnix:   c.NowUnix,
		Folded:    folded,
		Projected: sim.Project(folded, time.Unix(c.NowUnix, 0).UTC(), c.Tuning),
	}
}

func at(h, m int) time.Time {
	return T0.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

func genesis() sim.State { return sim.Genesis("pet1", T0) }

// Cases é o catálogo. Adicionar Kind novo ou regra nova sem adicionar caso
// aqui é o mesmo que não ter testado.
func Cases() []Case {
	t := sim.DefaultTuning()

	encontros := func(n int, passo time.Duration) []sim.Event {
		out := make([]sim.Event, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, sim.Event{
				ID:   "E" + string(rune('A'+i)),
				At:   T0.Add(time.Duration(i) * passo),
				Kind: sim.KindEncounter, PeerID: "p2",
			})
		}
		return out
	}

	base := []sim.Event{
		{ID: "01A", At: at(1, 0), Kind: sim.KindEffort, Kcal: 500, Zone: 3},
		{ID: "01B", At: at(2, 0), Kind: sim.KindInteract},
		{ID: "01C", At: at(3, 0), Kind: sim.KindSleep, Minutes: 420},
		{ID: "01D", At: at(4, 0), Kind: sim.KindEncounter, PeerID: "p2"},
	}

	// snapshot já foldado, pra provar que evento atrasado ainda entra
	snap := sim.Fold(genesis(), base, t)

	cs := []Case{
		{"genesis_sem_evento", t, genesis(), nil, at(1, 0).Unix()},
		{"effort_simples", t, genesis(), base[:1], at(2, 0).Unix()},
		{"sleep_simples", t, genesis(), base[2:3], at(4, 0).Unix()},
		{"interact_simples", t, genesis(), base[1:2], at(3, 0).Unix()},
		{"encounter_simples", t, genesis(), base[3:], at(5, 0).Unix()},
		{"lote_completo", t, genesis(), base, at(10, 0).Unix()},

		{"lote_fora_de_ordem", t, genesis(),
			[]sim.Event{base[3], base[0], base[2], base[1]}, at(10, 0).Unix()},

		{"lote_com_duplicatas", t, genesis(),
			append(append([]sim.Event{}, base...), base...), at(10, 0).Unix()},

		{"clamp_teto", t, genesis(), []sim.Event{
			{ID: "01A", At: at(1, 0), Kind: sim.KindEffort, Kcal: 65535, Zone: 1},
			{ID: "01B", At: at(2, 0), Kind: sim.KindSleep, Minutes: 65535},
		}, at(3, 0).Unix()},

		{"clamp_piso_abandono_30d", t, genesis(), base,
			T0.Add(30 * 24 * time.Hour).Unix()},

		{"cooldown_encontro_20_em_20min", t, genesis(),
			encontros(20, time.Minute), at(1, 0).Unix()},

		{"encontro_apos_cooldown", t, genesis(),
			encontros(2, 7*time.Hour), at(15, 0).Unix()},

		// O caso que motivou separar Fold de Project. O snapshot já parou no
		// último evento; um evento que aconteceu ANTES da última projeção
		// ainda tem que ser aplicado.
		{"evento_atrasado_sobre_snapshot", t, snap, []sim.Event{
			{ID: "01Z", At: at(9, 57), Kind: sim.KindEffort, Kcal: 500, Zone: 1},
		}, at(11, 0).Unix()},

		// Gaps que não fecham hora cheia. Se o CarrySec quebrar, fold
		// incremental deixa de bater com replay e este vetor muda.
		{"carry_sec_gaps_quebrados", t, genesis(), []sim.Event{
			{ID: "01A", At: at(0, 37), Kind: sim.KindInteract},
			{ID: "01B", At: at(1, 19), Kind: sim.KindInteract},
			{ID: "01C", At: at(2, 51), Kind: sim.KindInteract},
			{ID: "01D", At: at(4, 8), Kind: sim.KindInteract},
		}, at(5, 23).Unix()},
	}

	// evolução longa: growth suficiente pra atravessar os estágios
	longo := make([]sim.Event, 0, 300)
	for i := 0; i < 300; i++ {
		longo = append(longo, sim.Event{
			ID:   "L" + string(rune('A'+i/26)) + string(rune('A'+i%26)),
			At:   T0.Add(time.Duration(i) * 4 * time.Hour),
			Kind: sim.KindEffort, Kcal: 300, Zone: 2,
		})
	}
	cs = append(cs, Case{"evolucao_longa_300_efforts", t, genesis(), longo,
		T0.Add(300 * 4 * time.Hour).Unix()})

	return cs
}

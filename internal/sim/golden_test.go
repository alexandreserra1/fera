// Este teste é externo (package sim_test) de propósito: ele só pode enxergar
// a API pública, que é exatamente o que os outros runtimes enxergam.
package sim_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ale/fera/internal/sim"
	"github.com/ale/fera/internal/sim/vectors"
)

const dir = "testdata/vectors"

// Os vetores são o contrato entre servidor, WASM e firmware. Se este teste
// quebrar, ou você mudou uma regra de propósito (aí regenera com
// `go run ./cmd/gen-vectors -write` e sobe o SchemaVer), ou você quebrou o
// core sem perceber. Não regenere pra "consertar" o teste.
func TestGoldenVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("nenhum vetor em " + dir + "; rode go run ./cmd/gen-vectors -write")
	}

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var v vectors.Vector
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal(err)
			}

			folded := sim.Fold(v.Initial, v.Events, v.Tuning)
			if folded != v.Folded {
				t.Errorf("Fold divergiu do vetor\n  got: %+v\n want: %+v", folded, v.Folded)
			}

			proj := sim.Project(folded, time.Unix(v.NowUnix, 0).UTC(), v.Tuning)
			if proj != v.Projected {
				t.Errorf("Project divergiu do vetor\n  got: %+v\n want: %+v", proj, v.Projected)
			}

			if v.Folded.SchemaVer != sim.SchemaVer {
				t.Errorf("vetor é do schema %d, o core está no %d: regenere",
					v.Folded.SchemaVer, sim.SchemaVer)
			}
		})
	}
}

// Caso novo no catálogo sem arquivo gerado é o jeito silencioso de não testar.
func TestGoldenVectorsCobremTodosOsCasos(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	tem := make(map[string]bool, len(files))
	for _, f := range files {
		tem[filepath.Base(f)] = true
	}

	cases := vectors.Cases()
	for _, c := range cases {
		if !tem[c.Name+".json"] {
			t.Errorf("caso %q não tem vetor gerado; rode go run ./cmd/gen-vectors -write", c.Name)
		}
	}
	if len(files) != len(cases) {
		t.Errorf("%d arquivos pra %d casos: tem vetor órfão", len(files), len(cases))
	}
}

// Ler o vetor de volta do JSON tem que dar o mesmo State bit a bit. Foi por
// isto que o tempo no State virou int64: time.Time carrega location e
// monotônico e não sobrevive limpo a um round-trip.
func TestVetorSobreviveAoRoundTripJSON(t *testing.T) {
	for _, c := range vectors.Cases() {
		v := vectors.Compute(c)
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		var back vectors.Vector
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if back.Folded != v.Folded || back.Projected != v.Projected || back.Initial != v.Initial {
			t.Errorf("%s: round-trip mudou o estado", c.Name)
		}
		if got := sim.Fold(back.Initial, back.Events, back.Tuning); got != v.Folded {
			t.Errorf("%s: fold depois do round-trip divergiu\n got: %+v\nwant: %+v",
				c.Name, got, v.Folded)
		}
	}
}

// DefaultTuning é contrato, não detalhe: cada vetor carrega o Tuning que usou,
// então mexer no balanceamento padrão não quebra vetor nenhum. Se o firmware
// compilar um balanceamento e o servidor outro, os dois divergem em silêncio.
// Este teste é o que trava isso.
func TestDefaultTuningEstaPinado(t *testing.T) {
	b, err := os.ReadFile("testdata/default_tuning.json")
	if err != nil {
		t.Fatal(err)
	}
	var want sim.Tuning
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if got := sim.DefaultTuning(); got != want {
		t.Fatalf("DefaultTuning mudou. Isso altera o comportamento de todo device já\n"+
			"em campo, então exige bump de SchemaVer e regeneração dos vetores.\n"+
			" got: %+v\nwant: %+v", got, want)
	}
}

// Command gen-vectors escreve os golden vectors em internal/sim/testdata/vectors.
//
// Só escreve com -write. Regenerar tem que ser um ato deliberado: se o teste
// regenerasse sozinho, uma regressão no sim "consertaria" o vetor e o
// contrato entre runtimes viraria enfeite.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ale/fera/internal/sim"
	"github.com/ale/fera/internal/sim/vectors"
)

func main() {
	out := flag.String("out", "internal/sim/testdata/vectors", "diretório de saída")
	write := flag.Bool("write", false, "escreve os arquivos; sem isto só lista")
	flag.Parse()

	cases := vectors.Cases()

	if !*write {
		fmt.Printf("%d vetores (use -write pra gravar em %s):\n", len(cases), *out)
		for _, c := range cases {
			fmt.Printf("  %s\n", c.Name)
		}
		return
	}

	if err := run(*out, cases); err != nil {
		fmt.Fprintln(os.Stderr, "gen-vectors:", err)
		os.Exit(1)
	}
	fmt.Printf("%d vetores gravados em %s\n", len(cases), *out)
}

func run(dir string, cases []vectors.Case) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// limpa o que sobrou de um catálogo anterior, senão vetor renomeado vira
	// órfão e o teste passa contando um arquivo que ninguém mais gera
	velhos, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	for _, f := range velhos {
		if err := os.Remove(f); err != nil {
			return err
		}
	}

	// DefaultTuning também é contrato: se o device compilar um balanceamento
	// e o servidor outro, os dois divergem sem que nenhum vetor perceba,
	// porque cada vetor carrega o Tuning que usou.
	tb, err := json.MarshalIndent(sim.DefaultTuning(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "..", "default_tuning.json"), append(tb, '\n'), 0o644); err != nil {
		return err
	}

	for _, c := range cases {
		b, err := json.MarshalIndent(vectors.Compute(c), "", "  ")
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		p := filepath.Join(dir, c.Name+".json")
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return nil
}

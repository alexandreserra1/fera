// Command gen-frames escreve as telas douradas em internal/device/ui/testdata.
//
// Só grava com -write, e sem -write desenha no terminal. Os goldens são ASCII
// e não PNG de propósito: um render errado em PNG vira diff binário que
// ninguém consegue revisar, enquanto '.' e '#' você lê no terminal e no PR.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/ui"
)

func main() {
	out := flag.String("out", "internal/device/ui/testdata", "diretório de saída")
	write := flag.Bool("write", false, "grava os arquivos; sem isto, desenha no terminal")
	flag.Parse()

	frames := ui.Frames()

	if !*write {
		for _, f := range frames {
			b := display.NewBuffer(ui.Largura, ui.Altura)
			ui.Render(b, f.View)
			fmt.Printf("=== %s ===\n%s\n", f.Nome, b.String())
		}
		return
	}

	if err := grava(*out, frames); err != nil {
		fmt.Fprintln(os.Stderr, "gen-frames:", err)
		os.Exit(1)
	}
	fmt.Printf("%d telas gravadas em %s\n", len(frames), *out)
}

func grava(dir string, frames []ui.Frame) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	velhos, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return err
	}
	for _, f := range velhos {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	for _, f := range frames {
		b := display.NewBuffer(ui.Largura, ui.Altura)
		ui.Render(b, f.View)
		p := filepath.Join(dir, f.Nome+".txt")
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			return fmt.Errorf("%s: %w", f.Nome, err)
		}
	}
	return nil
}

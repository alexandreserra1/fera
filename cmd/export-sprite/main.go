// Command export-sprite escreve os bichos como PNG, pra editar em editor de
// pixel de verdade (Piskel, Pixilart, Aseprite) e trazer de volta com
// cmd/import-sprite.
//
// Existe porque desenhar 64x64 digitando '#' e '.' funciona pra ajustar, mas
// é péssimo pra criar. Editor de pixel mostra a grade, tem balde, espelho e
// desfazer — e o ciclo passa a ser: exporta, desenha, importa.
//
//	go run ./cmd/export-sprite -out /tmp/bichos          # os cinco, em 8x
//	go run ./cmd/export-sprite -estagio adulto -escala 1 # um, no tamanho real
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/sim"
)

var estagios = map[string]sim.Stage{
	"ovo": sim.StageOvo, "filhote": sim.StageFilhote, "jovem": sim.StageJovem,
	"adulto": sim.StageAdulto, "veterano": sim.StageVeterano,
}

func main() {
	out := flag.String("out", ".", "diretório de saída")
	estagio := flag.String("estagio", "", "só um estágio (ovo, filhote, jovem, adulto, veterano)")
	// 8x por padrão: 64x64 numa tela moderna é do tamanho de uma unha, e
	// editor de pixel abre no tamanho do arquivo.
	escala := flag.Int("escala", 8, "quantas vezes ampliar cada pixel")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "export-sprite:", err)
		os.Exit(1)
	}

	quais := estagios
	if *estagio != "" {
		st, ok := estagios[*estagio]
		if !ok {
			fmt.Fprintf(os.Stderr, "export-sprite: estágio %q desconhecido\n", *estagio)
			os.Exit(2)
		}
		quais = map[string]sim.Stage{*estagio: st}
	}

	for nome, st := range quais {
		caminho := filepath.Join(*out, nome+".png")
		f, err := os.Create(caminho)
		if err != nil {
			fmt.Fprintln(os.Stderr, "export-sprite:", err)
			os.Exit(1)
		}
		err = escrever(f, ui.SpriteDoEstagio(st), *escala)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "export-sprite:", err)
			os.Exit(1)
		}
		fmt.Println(caminho)
	}
}

// escrever põe o sprite num PNG em preto e branco.
//
// Preto pra pixel aceso, branco pro apagado: é o que qualquer editor de pixel
// abre sem configuração, e é o que o cmd/import-sprite espera de volta sem
// precisar de -inverter.
func escrever(w io.Writer, sp display.Sprite, escala int) error {
	if escala < 1 {
		escala = 1
	}
	img := image.NewGray(image.Rect(0, 0, int(sp.W)*escala, int(sp.H)*escala))
	for y := int16(0); y < sp.H; y++ {
		for x := int16(0); x < sp.W; x++ {
			c := color.Gray{Y: 255}
			if sp.At(x, y) {
				c = color.Gray{Y: 0}
			}
			for dy := range escala {
				for dx := range escala {
					img.Set(int(x)*escala+dx, int(y)*escala+dy, c)
				}
			}
		}
	}
	return png.Encode(w, img)
}

// Command import-sprite converte um PNG na arte ASCII que internal/device/ui
// usa.
//
// Existe pra que arte de terceiro (packs CC0 de 1-bit, como o
// github.com/Kuronons/FZ_graphics) entre no projeto sem ninguém redesenhar
// pixel a pixel — e pra que a arte continue revisável, porque a saída é o
// mesmo bloco de '.' e '#' que já está no sprites.go.
//
//	go run ./cmd/import-sprite -in bicho.png -nome adulto
//	go run ./cmd/import-sprite -in bicho.png -inverter -limiar 100
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"
)

type opcoes struct {
	// limiar separa aceso de apagado na luminância, 0..255.
	limiar uint32
	// inverter troca fundo e desenho. Arte de device mono costuma vir com
	// fundo escuro, e sem isto o bicho sai como o negativo dele.
	inverter bool
}

func main() {
	in := flag.String("in", "", "arquivo PNG de entrada")
	larg := flag.Int("w", 32, "largura de saída")
	alt := flag.Int("h", 32, "altura de saída")
	nome := flag.String("nome", "", "se dado, imprime como const Go pronta pro sprites.go")
	limiar := flag.Uint("limiar", 128, "corte de luminância, 0..255")
	inverter := flag.Bool("inverter", false, "troca fundo e desenho")
	flag.Parse()

	if *in == "" {
		fmt.Fprintln(os.Stderr, "uso: import-sprite -in arquivo.png [-w 32] [-h 32] [-nome adulto] [-inverter]")
		os.Exit(2)
	}
	b, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import-sprite:", err)
		os.Exit(1)
	}
	arte, err := converter(b, *larg, *alt, opcoes{limiar: uint32(*limiar), inverter: *inverter})
	if err != nil {
		fmt.Fprintln(os.Stderr, "import-sprite:", err)
		os.Exit(1)
	}

	if *nome != "" {
		fmt.Printf("const arte%s = `\n%s`\n", strings.Title(*nome), arte)
		return
	}
	fmt.Println(arte)
}

func converter(dados []byte, larg, alt int, o opcoes) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(dados))
	if err != nil {
		return "", fmt.Errorf("decodificar imagem: %w", err)
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return "", fmt.Errorf("imagem vazia")
	}

	// Recorta o centro do lado maior antes de reduzir. Esticar pra caber
	// distorceria o bicho, e um bicho achatado é pior que um bicho cortado.
	lado := b.Dx()
	if b.Dy() < lado {
		lado = b.Dy()
	}
	origem := image.Rect(
		b.Min.X+(b.Dx()-lado)/2, b.Min.Y+(b.Dy()-lado)/2,
		b.Min.X+(b.Dx()-lado)/2+lado, b.Min.Y+(b.Dy()-lado)/2+lado,
	)

	var sb strings.Builder
	sb.Grow((larg + 1) * alt)
	for y := range alt {
		if y > 0 {
			sb.WriteByte('\n')
		}
		for x := range larg {
			// Voto da maioria sobre a região de origem inteira. Amostrar um
			// pixel só perderia traço de 1px, que em arte 1-bit é a maior
			// parte do desenho.
			x0 := origem.Min.X + x*lado/larg
			x1 := origem.Min.X + (x+1)*lado/larg
			y0 := origem.Min.Y + y*lado/alt
			y1 := origem.Min.Y + (y+1)*lado/alt
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}

			var acesos, total int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					total++
					if aceso(img, sx, sy, o) {
						acesos++
					}
				}
			}
			if acesos*2 >= total {
				sb.WriteByte('#')
			} else {
				sb.WriteByte('.')
			}
		}
	}
	return sb.String(), nil
}

// aceso decide se o pixel faz parte do desenho. Pixel transparente é fundo:
// PNG de sprite quase sempre tem alfa, e tratar transparente como escuro
// pintaria o bicho inteiro.
func aceso(img image.Image, x, y int, o opcoes) bool {
	r, g, b, a := img.At(x, y).RGBA()
	if a>>8 < 128 {
		return false
	}
	// luminância aproximada, em 0..255
	lum := (r>>8)*30/100 + (g>>8)*59/100 + (b>>8)*11/100
	escuro := lum < o.limiar
	if o.inverter {
		return !escuro
	}
	return escuro
}

var _ = png.Encode // mantém image/png linkado pro decode

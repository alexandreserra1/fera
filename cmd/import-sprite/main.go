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

	// Recorte de tilesheet. Pack de terceiro quase nunca vem em arquivo por
	// sprite: vem numa grade. tile é o lado da célula, esp o espaçamento
	// entre elas, e (col, lin) a célula desejada.
	tile, esp, col, lin int

	// suavizar amplia com Scale2x em vez de repetir pixel. Arte de 16x16
	// ampliada 4x por repetição vira bloco de 4x4 e parece Minecraft;
	// Scale2x preenche os cantos internos e o resultado parece desenho.
	suavizar bool
}

func main() {
	in := flag.String("in", "", "arquivo PNG de entrada")
	larg := flag.Int("w", 32, "largura de saída")
	alt := flag.Int("h", 32, "altura de saída")
	nome := flag.String("nome", "", "se dado, imprime como const Go pronta pro sprites.go")
	limiar := flag.Uint("limiar", 128, "corte de luminância, 0..255")
	inverter := flag.Bool("inverter", false, "troca fundo e desenho")
	tile := flag.Int("tile", 0, "lado da célula, se a entrada for tilesheet")
	esp := flag.Int("esp", 0, "espaçamento entre células do tilesheet")
	col := flag.Int("col", 0, "coluna da célula")
	lin := flag.Int("lin", 0, "linha da célula")
	suavizar := flag.Bool("suavizar", false, "amplia com Scale2x em vez de repetir pixel")
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
	arte, err := converter(b, *larg, *alt, opcoes{
		limiar: uint32(*limiar), inverter: *inverter,
		tile: *tile, esp: *esp, col: *col, lin: *lin, suavizar: *suavizar,
	})
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

	var origem image.Rectangle
	if o.tile > 0 {
		passo := o.tile + o.esp
		x0 := b.Min.X + o.col*passo
		y0 := b.Min.Y + o.lin*passo
		origem = image.Rect(x0, y0, x0+o.tile, y0+o.tile)
		if !origem.In(b) {
			return "", fmt.Errorf("célula (%d,%d) fora da grade de %dx%d", o.col, o.lin, b.Dx(), b.Dy())
		}
	} else {
		// Recorta o centro do lado maior antes de reduzir. Esticar pra caber
		// distorceria o bicho, e um bicho achatado é pior que um cortado.
		lado := b.Dx()
		if b.Dy() < lado {
			lado = b.Dy()
		}
		origem = image.Rect(
			b.Min.X+(b.Dx()-lado)/2, b.Min.Y+(b.Dy()-lado)/2,
			b.Min.X+(b.Dx()-lado)/2+lado, b.Min.Y+(b.Dy()-lado)/2+lado,
		)
	}
	lado := origem.Dx()

	// Com -suavizar, amplia a origem com Scale2x até passar do tamanho de
	// saída, e só então amostra. Suavizar DEPOIS de reduzir não adiantaria:
	// o detalhe já teria sido perdido.
	if o.suavizar {
		grade := make([][]bool, lado)
		for y := range lado {
			grade[y] = make([]bool, lado)
			for x := range lado {
				grade[y][x] = aceso(img, origem.Min.X+x, origem.Min.Y+y, o)
			}
		}
		for len(grade) < alt || len(grade[0]) < larg {
			grade = scale2x(grade)
		}
		return amostrar(grade, larg, alt), nil
	}

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

// scale2x (EPX) dobra a resolução preenchendo cantos internos a partir dos
// quatro vizinhos. É o algoritmo clássico de ampliar pixel art: sem ele,
// ampliar é repetir pixel e a diagonal vira escada de blocos.
func scale2x(g [][]bool) [][]bool {
	h := len(g)
	if h == 0 {
		return [][]bool{}
	}
	w := len(g[0])
	out := make([][]bool, h*2)
	for i := range out {
		out[i] = make([]bool, w*2)
	}
	// Fora da borda GRAMPEIA no pixel mais próximo, não devolve vazio.
	// Devolvendo vazio, o canto de um bloco sólido vira buraco: os dois
	// vizinhos de fora "concordam" em ser fundo e o EPX preenche com fundo.
	em := func(x, y int) bool {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= w {
			x = w - 1
		}
		if y >= h {
			y = h - 1
		}
		return g[y][x]
	}
	for y := range h {
		for x := range w {
			p := g[y][x]
			a, b, c, d := em(x, y-1), em(x+1, y), em(x-1, y), em(x, y+1)
			e0, e1, e2, e3 := p, p, p, p
			if c == a && c != d && a != b {
				e0 = a
			}
			if a == b && a != c && b != d {
				e1 = b
			}
			if d == c && d != b && c != a {
				e2 = c
			}
			if b == d && b != a && d != c {
				e3 = d
			}
			out[y*2][x*2], out[y*2][x*2+1] = e0, e1
			out[y*2+1][x*2], out[y*2+1][x*2+1] = e2, e3
		}
	}
	return out
}

// amostrar reduz a grade pro tamanho pedido por voto da maioria.
func amostrar(g [][]bool, larg, alt int) string {
	h, w := len(g), len(g[0])
	var sb strings.Builder
	sb.Grow((larg + 1) * alt)
	for y := range alt {
		if y > 0 {
			sb.WriteByte('\n')
		}
		for x := range larg {
			x0, x1 := x*w/larg, (x+1)*w/larg
			y0, y1 := y*h/alt, (y+1)*h/alt
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var on, tot int
			for sy := y0; sy < y1 && sy < h; sy++ {
				for sx := x0; sx < x1 && sx < w; sx++ {
					tot++
					if g[sy][sx] {
						on++
					}
				}
			}
			if tot > 0 && on*2 >= tot {
				sb.WriteByte('#')
			} else {
				sb.WriteByte('.')
			}
		}
	}
	return sb.String()
}

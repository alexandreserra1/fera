package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
)

// importar existe pro teste de ida e volta: exportar e reimportar tem que
// devolver a mesma arte, senão editar no Piskel e trazer de volta perde pixel.
func importar(dados []byte, w, h int) ([][]bool, error) {
	img, _, err := image.Decode(bytes.NewReader(dados))
	if err != nil {
		return nil, fmt.Errorf("decodificar: %w", err)
	}
	b := img.Bounds()
	if b.Dx() != w || b.Dy() != h {
		return nil, fmt.Errorf("imagem %dx%d, esperado %dx%d", b.Dx(), b.Dy(), w, h)
	}
	out := make([][]bool, h)
	for y := range h {
		out[y] = make([]bool, w)
		for x := range w {
			r, _, _, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out[y][x] = r>>8 < 128
		}
	}
	return out, nil
}

// Package display é a abstração de tela do device.
//
// Nada aqui importa "machine". É por isso que o renderer inteiro roda com
// `go test` no Mac, sem placa ligada, e é a diferença entre depurar num
// terminal e depurar com println na serial.
package display

// Layout do framebuffer: row-major, 1 bit por pixel, MSB é o pixel mais à
// esquerda do byte.
//
// Row-major porque é o layout do Sharp memory LCD, que é a tela escolhida no
// docs/06. O SSD1306 de protótipo é page-major (um byte = 8 pixels VERTICAIS)
// e transpõe dentro do Show(). Transpor 1 KB custa microssegundos e mantém a
// esquisitice de um controlador específico dentro do driver dele, em vez de
// espalhada pelo renderer.
type Buffer struct {
	W, H   int16
	Stride int16 // bytes por linha
	Bits   []byte
}

func NewBuffer(w, h int16) *Buffer {
	stride := (w + 7) / 8
	return &Buffer{W: w, H: h, Stride: stride, Bits: make([]byte, int(stride)*int(h))}
}

// Set liga ou desliga um pixel. Fora da tela é no-op, não pânico: clipping é
// responsabilidade daqui, e não de cada chamador desenhando forma.
func (b *Buffer) Set(x, y int16, on bool) {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return
	}
	i := int(y)*int(b.Stride) + int(x/8)
	mask := byte(0x80 >> (x % 8))
	if on {
		b.Bits[i] |= mask
	} else {
		b.Bits[i] &^= mask
	}
}

func (b *Buffer) Get(x, y int16) bool {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return false
	}
	i := int(y)*int(b.Stride) + int(x/8)
	return b.Bits[i]&(0x80>>(x%8)) != 0
}

func (b *Buffer) Clear() {
	for i := range b.Bits {
		b.Bits[i] = 0
	}
}

// Fill preenche um retângulo. Usado pelas barras de atributo e pelas molduras.
func (b *Buffer) Fill(x, y, w, h int16, on bool) {
	for dy := int16(0); dy < h; dy++ {
		for dx := int16(0); dx < w; dx++ {
			b.Set(x+dx, y+dy, on)
		}
	}
}

func (b *Buffer) Rect(x, y, w, h int16, on bool) {
	if w <= 0 || h <= 0 {
		return
	}
	for dx := int16(0); dx < w; dx++ {
		b.Set(x+dx, y, on)
		b.Set(x+dx, y+h-1, on)
	}
	for dy := int16(0); dy < h; dy++ {
		b.Set(x, y+dy, on)
		b.Set(x+w-1, y+dy, on)
	}
}

func (b *Buffer) Blit(x, y int16, s Sprite) { b.BlitScaled(x, y, s, 1) }

// BlitScaled desenha o sprite ampliado por um inteiro. Existe pra que a arte
// caiba em 16x16 na flash e apareça em 32x32 na tela: quatro vezes menos bytes
// pra guardar e pra escrever à mão.
//
// Pixel a pixel de propósito. Um frame de 128x64 são 8 mil iterações num MCU
// de 240 MHz que desenha UM frame a cada 5 minutos em regime. Otimizar pra
// cópia alinhada seria trocar clareza por tempo que ninguém está esperando.
func (b *Buffer) BlitScaled(x, y int16, s Sprite, escala int16) {
	if escala < 1 {
		escala = 1
	}
	for sy := int16(0); sy < s.H; sy++ {
		for sx := int16(0); sx < s.W; sx++ {
			if !s.At(sx, sy) {
				continue // sprite mono é transparente no zero
			}
			b.Fill(x+sx*escala, y+sy*escala, escala, escala, true)
		}
	}
}

// Sprite é um bitmap mono no mesmo layout do Buffer. Bits aponta pra um array
// global, que no TinyGo fica na flash: fatiar não copia, então sprite nenhum
// ocupa RAM.
type Sprite struct {
	W, H   int16
	Stride int16
	Bits   []byte
}

func (s Sprite) At(x, y int16) bool {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return false
	}
	return s.Bits[int(y)*int(s.Stride)+int(x/8)]&(0x80>>(x%8)) != 0
}

// Display é o mínimo que o renderer precisa saber fazer. Três métodos: se
// crescer pra oito, alguma responsabilidade de driver vazou pra cá.
type Display interface {
	Clear()
	Blit(x, y int16, s Sprite)
	Show() error
}

// Buffered implementa tudo menos o Show. Todo driver embute isto e só precisa
// saber empurrar bytes pro barramento dele.
type Buffered struct{ Buf *Buffer }

func (d *Buffered) Clear() { d.Buf.Clear() }

// Framebuffer expõe o buffer pra quem precisa desenhar primitiva que a
// interface Display não cobre, como texto. Fica fora de Display de propósito:
// um driver que fale direto com o controlador, sem framebuffer local, não
// deveria ser obrigado a inventar um.
func (d *Buffered) Framebuffer() *Buffer { return d.Buf }

func (d *Buffered) Blit(x, y int16, s Sprite) { d.Buf.Blit(x, y, s) }

// Fake é o display de teste: guarda tudo em memória e conta os Show.
// Todo teste de UI do projeto roda em cima dele, no Mac, sem hardware.
type Fake struct {
	Buffered
	Shows int
}

func NewFake(w, h int16) *Fake {
	return &Fake{Buffered: Buffered{Buf: NewBuffer(w, h)}}
}

func (f *Fake) Show() error {
	f.Shows++
	return nil
}

// String desenha o buffer em ASCII. É o que torna um teste de render legível:
// quando quebra, o diff mostra o desenho errado, não um blob de bytes.
func (b *Buffer) String() string {
	out := make([]byte, 0, (int(b.W)+1)*int(b.H))
	for y := int16(0); y < b.H; y++ {
		for x := int16(0); x < b.W; x++ {
			if b.Get(x, y) {
				out = append(out, '#')
			} else {
				out = append(out, '.')
			}
		}
		out = append(out, '\n')
	}
	return string(out)
}

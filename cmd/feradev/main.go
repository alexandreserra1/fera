// Command feradev roda a FERA no terminal do Mac.
//
// Não é demo: é a bancada de calibragem. Todo valor do sim.DefaultTuning é
// chute marcado com "TODO: calibrar", e não dá pra calibrar balanceamento
// lendo constante. Com o relógio acelerado dá pra ver um mês passar em meio
// minuto e sentir se o bicho definha rápido demais.
//
// Roda o MESMO internal/sim, o MESMO internal/device/loop e a MESMA lógica de
// flash que vão pro ESP32. O que muda é só o hal e o driver de tela.
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/hal"
	"github.com/ale/fera/internal/device/loop"
	"github.com/ale/fera/internal/device/net"
	"github.com/ale/fera/internal/device/store"
	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/sim"
)

const (
	setores    = 7 // 2 estado + 1 credencial + 4 fila
	setorBytes = 4096
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "feradev:", err)
		os.Exit(1)
	}
}

func run() error {
	caminho := flag.String("flash", "fera.flash", "arquivo que faz as vezes da flash do device")
	velocidade := flag.Float64("velocidade", 600, "quantas vezes o tempo corre mais rápido (600 = 10min por segundo)")
	pet := flag.String("pet", "11111111-1111-1111-1111-111111111111", "pet_id quando roda sem -api")
	api := flag.String("api", "", "URL da API (ex: http://localhost:8080). Vazio roda offline.")
	flag.Parse()

	flash, err := AbrirFileFlash(*caminho, setores, setorBytes)
	if err != nil {
		return err
	}
	defer flash.Close()

	st, err := store.Open(flash, setorBytes, setores)
	if err != nil {
		return err
	}

	restaura, _ := terminalCru()
	defer restaura()

	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, os.Interrupt, syscall.SIGTERM)

	h := novoMacHAL(*velocidade)
	tela := &telaTerminal{Buffered: display.Buffered{Buf: display.NewBuffer(ui.Largura, ui.Altura)}, h: h}

	sinc, petID, err := ligarNaAPI(st, *api, *pet)
	if err != nil {
		return err
	}

	cfg := loop.Padrao()
	cfg.PetID = petID
	cfg.IntervaloSync = 12 * time.Hour
	cfg.Entropia = entropiaReal

	l, err := loop.New(h, tela, st, sinc, cfg)
	if err != nil {
		return err
	}
	tela.l, tela.sinc = l, sinc

	go lerTeclas(h)

	// desenha o estado inicial antes da primeira espera
	tela.pinta()

	for {
		select {
		case <-sinais:
			restaura()
			fmt.Print("\r\n")
			return nil
		default:
		}
		if h.Saiu() {
			restaura()
			fmt.Print("\r\n")
			return nil
		}
		if err := l.Passo(); err != nil {
			return err
		}
	}
}

func entropiaReal() [10]byte {
	var b [10]byte
	_, _ = rand.Read(b[:])
	return b
}

// contador embrulha o sincronizador só pra que a tela mostre o que está
// acontecendo. Sem isto não dá pra ver, rodando, se o sync está indo.
type contador struct {
	dentro    loop.Sincronizador
	Chamadas  int
	Enviados  int
	UltimoErr error
}

func (c *contador) Sync(pend []sim.Event) (int, error) {
	c.Chamadas++
	n, err := c.dentro.Sync(pend)
	c.Enviados += n
	c.UltimoErr = err
	return n, err
}

// semRede é o modo offline: nada é marcado como enviado, então nenhum evento
// se perde e a fila acumula até alguém apontar pra uma API.
type semRede struct{}

func (semRede) Sync([]sim.Event) (int, error) { return 0, nil }

// ligarNaAPI resolve a credencial: usa a que já está na flash, ou registra e
// grava. O token aparece UMA vez na resposta do register, então ele vai pra
// flash antes de qualquer outra coisa acontecer.
func ligarNaAPI(st *store.Store, api, petPadrao string) (*contador, string, error) {
	if api == "" {
		return &contador{dentro: semRede{}}, petPadrao, nil
	}

	creds, err := st.LoadCreds()
	if err == store.ErrVazio || creds.BaseURL != api {
		novas, err := net.Registrar(api, net.Opcoes{})
		if err != nil {
			return nil, "", fmt.Errorf("registrar em %s: %w", api, err)
		}
		creds = store.Creds{
			BaseURL: novas.BaseURL, PetID: novas.PetID,
			DeviceID: novas.DeviceID, Token: novas.Token,
		}
		if err := st.SaveCreds(creds); err != nil {
			return nil, "", err
		}
		fmt.Printf("registrado: pet %s\r\n", creds.PetID)
	} else if err != nil {
		return nil, "", err
	}

	// Com DeviceID o cliente ASSINA em vez de mandar o token. É o caminho do
	// device de verdade: ver internal/sig e docs/06.
	cli := net.New(net.Credenciais{
		BaseURL: creds.BaseURL, PetID: creds.PetID,
		DeviceID: creds.DeviceID, Token: creds.Token,
	}, net.Opcoes{})
	return &contador{dentro: cli}, creds.PetID, nil
}

// telaTerminal é o "driver de display" do Mac: em vez de empurrar bytes por
// SPI, escreve o framebuffer no terminal. Implementa a mesma interface que o
// driver do Sharp vai implementar.
type telaTerminal struct {
	display.Buffered
	h    *macHAL
	l    *loop.Loop
	sinc *contador
}

func (t *telaTerminal) Show() error {
	t.pinta()
	return nil
}

func (t *telaTerminal) pinta() {
	var b strings.Builder
	b.WriteString("\033[H\033[2J") // topo e limpa

	b.WriteString("  FERA  ")
	b.WriteString(t.h.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "   tempo x%.0f   bateria %d%%\r\n", t.h.velocidade, t.h.Bateria())
	b.WriteString("  " + strings.Repeat("─", ui.Largura/2) + "\r\n")

	// duas colunas de pixel viram um caractere, pra tela não ficar deitada
	for y := int16(0); y < ui.Altura; y += 2 {
		b.WriteString("  ")
		for x := int16(0); x < ui.Largura; x += 2 {
			cima := t.Buf.Get(x, y) || t.Buf.Get(x+1, y)
			baixo := t.Buf.Get(x, y+1) || t.Buf.Get(x+1, y+1)
			switch {
			case cima && baixo:
				b.WriteString("█")
			case cima:
				b.WriteString("▀")
			case baixo:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\r\n")
	}

	b.WriteString("  " + strings.Repeat("─", ui.Largura/2) + "\r\n")
	if t.l != nil {
		v := t.l.Vista()
		fmt.Fprintf(&b, "  growth %d   syncs %d   enviados %d   perdidos %d\r\n",
			v.Growth, t.sinc.Chamadas, t.sinc.Enviados, t.l.PerdidosNaFila)
		if t.sinc.UltimoErr != nil {
			fmt.Fprintf(&b, "  sync: %v\r\n", t.sinc.UltimoErr)
		}
	}
	b.WriteString("  [a] alimentar   [i] interagir   [m] menu   [b] bateria   [q] sair\r\n")

	fmt.Print(b.String())
}

// terminalCru põe o terminal em modo caractere a caractere, pra que apertar
// 'a' seja um botão e não exija Enter. Volta ao normal na saída, inclusive no
// Ctrl-C: terminal deixado em modo cru é sessão inutilizada.
func terminalCru() (func(), error) {
	stty := func(args ...string) error {
		c := exec.Command("stty", args...)
		c.Stdin = os.Stdin
		return c.Run()
	}
	if err := stty("cbreak", "-echo"); err != nil {
		// stdin não é terminal (pipe, CI, redirecionamento). Segue em modo
		// linha: as teclas passam a exigir Enter, mas o programa roda. Falhar
		// aqui impediria até de testar o feradev com entrada canalizada.
		return func() {}, nil
	}
	return func() { _ = stty("sane") }, nil
}

func lerTeclas(h *macHAL) {
	var b [1]byte
	for {
		n, err := os.Stdin.Read(b[:])
		if err != nil || n == 0 {
			return
		}
		switch b[0] {
		case 'a':
			h.tecla(hal.BotaoAlimentar)
		case 'i':
			h.tecla(hal.BotaoInteragir)
		case 'm':
			h.tecla(hal.BotaoMenu)
		case 'b':
			h.baixarBateria()
		case 'q', 3: // 3 = Ctrl-C
			h.pedirSaida()
		}
	}
}

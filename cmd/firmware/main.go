// Command firmware é o binário do device.
//
// Wiring, e só. A máquina de estados mora em internal/device/loop justamente
// pra que ela seja testável; aqui não há regra nenhuma pra testar.
//
// AINDA NÃO RODA NUMA PLACA. O hal e a tela abaixo são esqueletos, porque sem
// hardware na mesa um driver só pode ser verificado por compilação, e driver
// que só compila é dívida disfarçada de progresso. O que este binário entrega
// hoje é real e verificável: ele prova que o programa INTEIRO (sim, ui,
// display, store, input, ulid, loop) linka pro Xtensa e cabe no orçamento de
// RAM do docs/06.
//
//	tinygo build -size=short -target=xiao-esp32s3 ./cmd/firmware
package main

import (
	"time"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/hal"
	"github.com/ale/fera/internal/device/loop"
	feranet "github.com/ale/fera/internal/device/net"
	"github.com/ale/fera/internal/device/store"
	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/sim"
)

const (
	setores    = 7 // 2 estado + 1 credencial + 4 fila
	setorBytes = 4096
)

// Tudo alocado no init, nada dentro do laço: o GC conservativo do TinyGo só
// fica quieto se o regime não alocar.
var (
	fb                      = display.NewBuffer(ui.Largura, ui.Altura)
	tela                    = &telaEsqueleto{Buffered: display.Buffered{Buf: fb}}
	relo                    = &halEsqueleto{}
	rede loop.Sincronizador = semRede{}
)

func main() {
	st, err := store.Open(flashEsqueleto{}, setorBytes, setores)
	if err != nil {
		println("store:", err.Error())
		return
	}

	cfg := loop.Padrao()
	cfg.Entropia = entropiaHardware

	// Credencial vem do register e mora na flash. Sem ela o device roda
	// offline: o bicho vive, a fila acumula, e nada se perde.
	if creds, err := st.LoadCreds(); err == nil {
		cfg.PetID = creds.PetID
		rede = feranet.New(feranet.Credenciais{
			BaseURL: creds.BaseURL, PetID: creds.PetID,
			DeviceID: creds.DeviceID, Token: creds.Token,
		}, feranet.Opcoes{})
	} else {
		cfg.PetID = "00000000-0000-0000-0000-000000000000"
	}

	l, err := loop.New(relo, tela, st, rede, cfg)
	if err != nil {
		println("loop:", err.Error())
		return
	}
	if err := l.Rodar(nil); err != nil {
		println("rodar:", err.Error())
	}
}

// TODO(placa): trocar por machine.GetRNG() do ESP32-S3, que tem RNG de
// hardware. Contador fixo aqui geraria ULID previsível e colidiria entre dois
// devices, o que quebraria a idempotência por ULID.
func entropiaHardware() [10]byte { return [10]byte{} }

// TODO(placa): hal de verdade. Este é o ÚNICO lugar do device que vai importar
// "machine": Now sai do RTC, Sleep do deep sleep, Botoes do wake por
// interrupção e Bateria do ADC.
type halEsqueleto struct{ t time.Time }

func (h *halEsqueleto) Now() time.Time { return h.t }
func (h *halEsqueleto) Sleep(d time.Duration) hal.Motivo {
	h.t = h.t.Add(d)
	return hal.MotivoTimer
}
func (h *halEsqueleto) Botoes() []hal.Botao { return nil }
func (h *halEsqueleto) Bateria() uint8      { return 100 }

// TODO(placa): driver do Sharp LS013B7DH03 por SPI. O Blit e o Clear já vêm
// do display.Buffered; falta só empurrar o framebuffer pro vidro.
type telaEsqueleto struct{ display.Buffered }

func (t *telaEsqueleto) Show() error { return nil }

// TODO(placa): NVS do ESP32 via tinygo.org/x/drivers.
type flashEsqueleto struct{}

func (flashEsqueleto) Read(_ int64, p []byte) error {
	for i := range p {
		p[i] = 0xFF
	}
	return nil
}
func (flashEsqueleto) Write(int64, []byte) error { return nil }
func (flashEsqueleto) Erase(int64) error         { return nil }

// semRede é o sincronizador enquanto o device não tem credencial gravada.
// Devolve zero enviados, então nada é marcado como sincronizado e nenhum
// evento se perde: os pendentes esperam na fila.
type semRede struct{}

func (semRede) Sync([]sim.Event) (int, error) { return 0, nil }

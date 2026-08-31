//go:build wasm

// Command wasm é a FERA no navegador.
//
// Ele NÃO reimplementa nada. Roda o mesmo internal/sim, o mesmo
// internal/device/ui e o mesmo internal/device/display que vão pro ESP32, e
// exporta o FRAMEBUFFER pro JavaScript. A tela do navegador mostra pixel por
// pixel o que a placa vai mostrar.
//
// Isso é o terceiro alvo do "um core, três alvos" do docs/00, e até agora
// só Go e TinyGo tinham sido verificados.
//
//	tinygo build -o web/fera.wasm -target=wasm ./cmd/wasm
package main

import (
	"syscall/js"
	"time"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/device/ui"
	"github.com/ale/fera/internal/sim"
)

var (
	tuning = sim.DefaultTuning()
	estado sim.State
	// Framebuffer alocado uma vez, igual ao device. A página passa um
	// Uint8Array e a gente copia pra dentro dele: nenhuma alocação por quadro.
	fb = display.NewBuffer(ui.Largura, ui.Altura)
)

func main() {
	js.Global().Set("feraNovo", js.FuncOf(novo))
	js.Global().Set("feraEvento", js.FuncOf(evento))
	js.Global().Set("feraQuadro", js.FuncOf(quadro))
	js.Global().Set("feraLargura", js.ValueOf(int(ui.Largura)))
	js.Global().Set("feraAltura", js.ValueOf(int(ui.Altura)))

	// Sem isto o runtime encerra e as funções somem.
	select {}
}

// novo(petID, nascimentoUnix) recomeça o bicho.
func novo(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return nil
	}
	estado = sim.Genesis(args[0].String(), time.Unix(int64(args[1].Int()), 0).UTC())
	return nil
}

// evento(id, kind, atUnix, kcal, zone, minutes) aplica UM evento.
//
// A página guarda o log de eventos e reaplica na carga. É event sourcing de
// verdade, igual ao servidor e ao device: o estado nunca é serializado, só
// derivado. Isso também evita encoding/json no WASM, que incharia o binário.
func evento(_ js.Value, args []js.Value) any {
	if len(args) < 6 {
		return nil
	}
	kind, ok := sim.KindFromName(args[1].String())
	if !ok {
		return false
	}
	estado = sim.Fold(estado, []sim.Event{{
		ID:      args[0].String(),
		At:      time.Unix(int64(args[2].Int()), 0).UTC(),
		Kind:    kind,
		Kcal:    uint16(args[3].Int()),
		Zone:    uint8(args[4].Int()),
		Minutes: uint16(args[5].Int()),
	}}, tuning)
	return true
}

// quadro(agoraUnix, destino) desenha e devolve o que a tela mostra.
//
// destino é um Uint8Array de feraLargura*feraAltura/8 bytes que a página
// aloca uma vez. O framebuffer é o MESMO formato do device: row-major, 1 bit
// por pixel, MSB à esquerda.
func quadro(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return nil
	}
	v := sim.Project(estado, time.Unix(int64(args[0].Int()), 0).UTC(), tuning)
	ui.Render(fb, v)
	js.CopyBytesToJS(args[1], fb.Bits)

	return map[string]any{
		"stage":   ui.NomeDoEstagio(v.Stage),
		"trait":   ui.NomeDoTraco(v.Trait),
		"growth":  int(v.Growth),
		"vigor":   int(sim.Pct(v.Stats.Vigor)),
		"animo":   int(sim.Pct(v.Stats.Animo)),
		"saude":   int(sim.Pct(v.Stats.Saude)),
		"vinculo": int(sim.Pct(v.Stats.Vinculo)),
	}
}

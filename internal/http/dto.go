// Package httpapi é a borda HTTP.
//
// O diretório é internal/http (como no docs/01) mas o pacote se chama httpapi:
// senão todo arquivo daqui precisaria aliasar net/http, que é justamente o
// import mais usado da camada.
//
// Regra da camada: DTO nunca é o tipo do domínio. A conversão e a validação
// acontecem aqui, na borda, antes de virar sim.Event.
package httpapi

import (
	"fmt"
	"time"

	"github.com/ale/fera/internal/sim"
)

const (
	maxEventos     = 200
	maxBody        = 1 << 20 // 1 MB
	futuroTolerado = 24 * time.Hour
	passadoAceito  = 90 * 24 * time.Hour
)

// A tabela de nomes de kind mora no sim, não aqui. O device também serializa
// evento, e duas cópias divergiriam no primeiro Kind novo.

type ingestRequest struct {
	Events []eventDTO `json:"events"`
}

type eventDTO struct {
	ID      string     `json:"id"`
	Kind    string     `json:"kind"`
	At      time.Time  `json:"at"`
	Payload payloadDTO `json:"payload"`
}

type payloadDTO struct {
	Kcal    uint16 `json:"kcal,omitempty"`
	Zone    uint8  `json:"zone,omitempty"`
	Minutes uint16 `json:"minutes,omitempty"`
	PeerID  string `json:"peer_id,omitempty"`
}

type ingestResponse struct {
	Accepted   int   `json:"accepted"`
	Duplicates int   `json:"duplicates"`
	Rejected   int   `json:"rejected"`
	Cursor     int64 `json:"cursor"`
}

type petResponse struct {
	PetID  string   `json:"pet_id"`
	Stage  string   `json:"stage"`
	Trait  string   `json:"trait"`
	Growth uint32   `json:"growth"`
	Stats  statsDTO `json:"stats"`
}

// Atributos vão 0..100 no wire. GET /v1/pets/{id} devolve sim.View, que é o
// tipo de exibição; quem precisa de precisão de centésimo é o app, e o app
// roda o próprio sim em WASM.
type statsDTO struct {
	Vigor   uint8 `json:"vigor"`
	Animo   uint8 `json:"animo"`
	Saude   uint8 `json:"saude"`
	Vinculo uint8 `json:"vinculo"`
}

var nomePorStage = map[sim.Stage]string{
	sim.StageOvo: "ovo", sim.StageFilhote: "filhote", sim.StageJovem: "jovem",
	sim.StageAdulto: "adulto", sim.StageVeterano: "veterano",
}

var nomePorTrait = map[sim.Trait]string{
	sim.TraitNeutro: "neutro", sim.TraitTeimoso: "teimoso", sim.TraitAgitado: "agitado",
	sim.TraitCalmo: "calmo", sim.TraitFerino: "ferino",
}

func fromView(v sim.View) petResponse {
	return petResponse{
		PetID:  v.PetID,
		Stage:  nomePorStage[v.Stage],
		Trait:  nomePorTrait[v.Trait],
		Growth: v.Growth,
		Stats: statsDTO{
			Vigor:   sim.Pct(v.Stats.Vigor),
			Animo:   sim.Pct(v.Stats.Animo),
			Saude:   sim.Pct(v.Stats.Saude),
			Vinculo: sim.Pct(v.Stats.Vinculo),
		},
	}
}

// validate cuida do que é estrutural: lote vazio ou grande demais derruba a
// requisição inteira, porque não dá pra adivinhar a intenção. Sem lib de
// validação por tag: é uma função e ela cabe na tela.
func (r ingestRequest) validate() error {
	if len(r.Events) == 0 {
		return fmt.Errorf("lote vazio")
	}
	if len(r.Events) > maxEventos {
		return fmt.Errorf("lote com %d eventos, máximo %d", len(r.Events), maxEventos)
	}
	return nil
}

// toDomain converte evento a evento e devolve o que foi aceito mais os IDs
// rejeitados.
//
// Rejeição é POR EVENTO, não por lote, de propósito. Derrubar o lote inteiro
// por causa de um evento torto faria um device com um único registro ruim
// travar o sync pra sempre, tentando o mesmo lote até o fim dos tempos.
//
// Relógio adiantado demais é clampado pro agora em vez de rejeitado: device
// sem RTC mente depois de reboot, e o dado do treino é bom mesmo quando o
// carimbo de tempo não é. Clampa pro agora e não pro teto porque o drift é
// espúrio: o relógio confiável nessa hora é o do servidor.
func (r ingestRequest) toDomain(now time.Time) ([]sim.Event, []string) {
	evs := make([]sim.Event, 0, len(r.Events))
	var rejeitados []string

	for _, d := range r.Events {
		kind, ok := sim.KindFromName(d.Kind)
		if d.ID == "" || !ok || d.At.IsZero() {
			rejeitados = append(rejeitados, d.ID)
			continue
		}
		if d.At.Before(now.Add(-passadoAceito)) {
			rejeitados = append(rejeitados, d.ID)
			continue
		}

		at := d.At.UTC()
		if at.After(now.Add(futuroTolerado)) {
			at = now.UTC()
		}

		evs = append(evs, sim.Event{
			ID: d.ID, At: at, Kind: kind,
			Kcal: d.Payload.Kcal, Zone: d.Payload.Zone,
			Minutes: d.Payload.Minutes, PeerID: d.Payload.PeerID,
		})
	}
	return evs, rejeitados
}

type eventsResponse struct {
	Events []eventDTO `json:"events"`
	Cursor int64      `json:"cursor"`
}

// fromDomain é o caminho de volta do toDomain. Evento sem nome de kind no wire
// não pode sair como string vazia: seria um evento que o cliente não consegue
// reenviar. TestTodoKindTemNomeNoWire trava isso.
func fromDomain(evs []sim.Event) []eventDTO {
	out := make([]eventDTO, 0, len(evs))
	for _, ev := range evs {
		out = append(out, eventDTO{
			ID:   ev.ID,
			Kind: sim.KindName(ev.Kind),
			At:   ev.At.UTC(),
			Payload: payloadDTO{
				Kcal: ev.Kcal, Zone: ev.Zone,
				Minutes: ev.Minutes, PeerID: ev.PeerID,
			},
		})
	}
	return out
}

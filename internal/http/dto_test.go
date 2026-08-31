package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ale/fera/internal/sim"
)

// O contrato só devolve a CONTAGEM de rejeitados, então o valor pro qual o
// relógio adiantado foi clampado não aparece na resposta. Aqui aparece.
func TestRelogioAdiantadoEhClampadoProAgora(t *testing.T) {
	req := ingestRequest{Events: []eventDTO{
		{ID: "01JF", Kind: "effort", At: agora.Add(500 * time.Hour), Payload: payloadDTO{Kcal: 300, Zone: 2}},
	}}

	evs, rej := req.toDomain(agora)
	if len(rej) != 0 {
		t.Fatalf("evento com relógio adiantado foi rejeitado em vez de clampado")
	}
	if !evs[0].At.Equal(agora) {
		t.Errorf("At = %v, esperado clamp pro agora (%v)", evs[0].At, agora)
	}
}

// Dentro da tolerância nada é tocado: clampar um evento legítimo de daqui a
// uma hora bagunçaria a ordem do fold sem motivo.
func TestFuturoDentroDaToleranciaPassaIntacto(t *testing.T) {
	quaseLa := agora.Add(futuroTolerado - time.Minute)
	req := ingestRequest{Events: []eventDTO{{ID: "01JF", Kind: "interact", At: quaseLa}}}

	evs, rej := req.toDomain(agora)
	if len(rej) != 0 || len(evs) != 1 {
		t.Fatalf("aceitos=%d rejeitados=%d, esperado 1 e 0", len(evs), len(rej))
	}
	if !evs[0].At.Equal(quaseLa) {
		t.Errorf("At = %v, esperado intacto (%v)", evs[0].At, quaseLa)
	}
}

func TestLoteAcimaDoTetoEhRecusadoInteiro(t *testing.T) {
	evs := make([]eventDTO, maxEventos+1)
	for i := range evs {
		evs[i] = eventDTO{ID: "x", Kind: "interact", At: agora}
	}
	if err := (ingestRequest{Events: evs}).validate(); err == nil {
		t.Errorf("lote com %d eventos passou pela validação", len(evs))
	}
	if err := (ingestRequest{Events: evs[:maxEventos]}).validate(); err != nil {
		t.Errorf("lote com exatamente %d eventos foi recusado: %v", maxEventos, err)
	}
}

// A tabela vive no sim; aqui só se confere que a borda de fato a usa, em vez
// de ter reintroduzido uma cópia local.
func TestKindVaiEVoltaNoWire(t *testing.T) {
	for k := sim.KindEffort; k <= sim.KindEncounter; k++ {
		nome := sim.KindName(k)
		req := ingestRequest{Events: []eventDTO{{ID: "01JA", Kind: nome, At: agora}}}
		evs, rej := req.toDomain(agora)
		if len(rej) != 0 || len(evs) != 1 {
			t.Fatalf("kind %q foi rejeitado na borda", nome)
		}
		if evs[0].Kind != k {
			t.Errorf("%q virou kind %d, esperado %d", nome, evs[0].Kind, k)
		}
		if volta := fromDomain(evs)[0].Kind; volta != nome {
			t.Errorf("kind %d saiu como %q, esperado %q", k, volta, nome)
		}
	}
}

// Stage e Trait novos no sim sem nome no wire virariam string vazia na
// resposta, silenciosamente. Este teste é o que impede isso.
func TestTodoStageETraitTemNomeNoWire(t *testing.T) {
	for s := sim.StageOvo; s <= sim.StageVeterano; s++ {
		if nomePorStage[s] == "" {
			t.Errorf("stage %d não tem nome no wire", s)
		}
	}
	for tr := sim.TraitNeutro; tr <= sim.TraitFerino; tr++ {
		if nomePorTrait[tr] == "" {
			t.Errorf("trait %d não tem nome no wire", tr)
		}
	}
}

// Evento sem id não pode entrar: o ULID é a identidade que faz a idempotência
// funcionar, e um evento sem ele viraria duplicata infinita.
func TestEventoSemIDEhRejeitado(t *testing.T) {
	req := ingestRequest{Events: []eventDTO{
		{ID: "", Kind: "interact", At: agora},
		{ID: "01JA", Kind: "interact", At: agora},
	}}
	evs, rej := req.toDomain(agora)
	if len(evs) != 1 || len(rej) != 1 {
		t.Errorf("aceitos=%d rejeitados=%d, esperado 1 e 1", len(evs), len(rej))
	}
}

// O pull devolve evento que o cliente pode reenviar tal e qual. Se ida e volta
// não fossem simétricas, um device que puxasse e reenviasse geraria evento
// diferente do original e a idempotência por ULID não salvaria.
func TestPullEIngestSaoSimetricos(t *testing.T) {
	original := []sim.Event{
		{ID: "01JA", At: agora, Kind: sim.KindEffort, Kcal: 500, Zone: 3},
		{ID: "01JB", At: agora, Kind: sim.KindSleep, Minutes: 420},
		{ID: "01JC", At: agora, Kind: sim.KindInteract},
		{ID: "01JD", At: agora, Kind: sim.KindEncounter, PeerID: "p2"},
	}

	volta, rej := ingestRequest{Events: fromDomain(original)}.toDomain(agora)
	if len(rej) != 0 {
		t.Fatalf("o que saiu do pull foi rejeitado na volta: %v", rej)
	}
	if len(volta) != len(original) {
		t.Fatalf("voltaram %d de %d eventos", len(volta), len(original))
	}
	for i := range original {
		if volta[i] != original[i] {
			t.Errorf("evento %d não sobreviveu ao round-trip\n got: %+v\nwant: %+v",
				i, volta[i], original[i])
		}
	}
}

// A validação de UUID no handler virou inalcançável nas rotas autenticadas: o
// requireOwnPet barra antes, porque o pet_id do device sempre é um UUID gerado
// pelo servidor. Ela fica como cinto e suspensório, pra que uma rota futura
// montada sem o requireOwnPet não entregue id torto ao pgx e devolva erro de
// driver pro cliente. Como não dá pra alcançá-la pelo router, testa direto.
func TestPetIDTortoParaAntesDoPgx(t *testing.T) {
	r := chi.NewRouter()
	var chegou bool
	r.Get("/v1/pets/{petID}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := petIDDaURL(w, r); ok {
			chegou = true
		}
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/pets/nao-sou-uuid", nil))

	if chegou {
		t.Error("pet_id torto passou pela validação")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, esperado 400", rec.Code)
	}
	var body errBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != codeInvalidPetID {
		t.Errorf("code = %q, esperado %q", body.Error.Code, codeInvalidPetID)
	}
}

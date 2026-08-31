package net

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ale/fera/internal/sim"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

func evs(n int) []sim.Event {
	out := make([]sim.Event, 0, n)
	for i := range n {
		out = append(out, sim.Event{
			ID:   "01J" + strings.Repeat("A", 22) + string(rune('A'+i%26)),
			At:   t0.Add(time.Duration(i) * time.Minute),
			Kind: sim.KindEffort, Kcal: uint16(100 + i), Zone: uint8(1 + i%5),
		})
	}
	return out
}

// O device não pode usar encoding/json (reflection infla o binário), então a
// serialização é escrita à mão. Este teste confere que ela produz JSON válido
// e no formato do api-contract.
func TestLoteSaiNoFormatoDoContrato(t *testing.T) {
	corpo := codificaLote(nil, []sim.Event{
		{ID: "01JA", At: t0, Kind: sim.KindEffort, Kcal: 420, Zone: 3},
		{ID: "01JB", At: t0.Add(time.Hour), Kind: sim.KindSleep, Minutes: 430},
		{ID: "01JC", At: t0.Add(2 * time.Hour), Kind: sim.KindEncounter, PeerID: "01JPEER"},
	})

	var lido struct {
		Events []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			At      string `json:"at"`
			Payload struct {
				Kcal    int    `json:"kcal"`
				Zone    int    `json:"zone"`
				Minutes int    `json:"minutes"`
				PeerID  string `json:"peer_id"`
			} `json:"payload"`
		} `json:"events"`
	}
	if err := json.Unmarshal(corpo, &lido); err != nil {
		t.Fatalf("o JSON escrito à mão não é válido: %v\n%s", err, corpo)
	}
	if len(lido.Events) != 3 {
		t.Fatalf("%d eventos no corpo, esperado 3", len(lido.Events))
	}
	if lido.Events[0].Kind != "effort" || lido.Events[0].Payload.Kcal != 420 {
		t.Errorf("evento 0 saiu errado: %+v", lido.Events[0])
	}
	if lido.Events[1].At != "2026-08-22T07:00:00Z" {
		t.Errorf("at = %q, esperado RFC3339 em UTC", lido.Events[1].At)
	}
	if lido.Events[2].Payload.PeerID != "01JPEER" {
		t.Errorf("peer_id não sobreviveu: %+v", lido.Events[2].Payload)
	}
}

// O formato de tempo é escrito à mão pra não puxar mais do pacote time do que
// o necessário. Tem que dar exatamente o que o time.Format daria.
func TestFormatoDeTempoBateComOTimeFormat(t *testing.T) {
	for _, at := range []time.Time{
		t0,
		time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2099, 10, 20, 9, 8, 7, 0, time.UTC),
	} {
		want := at.UTC().Format(time.RFC3339)
		got := string(escreveRFC3339(nil, at))
		if got != want {
			t.Errorf("escreveRFC3339 = %q, time.Format = %q", got, want)
		}
	}
}

func TestAspasNoIDNaoQuebramOJSON(t *testing.T) {
	corpo := codificaLote(nil, []sim.Event{
		{ID: `pe"ri\goso`, At: t0, Kind: sim.KindInteract},
	})
	var lido struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	if err := json.Unmarshal(corpo, &lido); err != nil {
		t.Fatalf("caractere especial quebrou o JSON: %v\n%s", err, corpo)
	}
	if lido.Events[0].ID != `pe"ri\goso` {
		t.Errorf("id = %q, esperado o original", lido.Events[0].ID)
	}
}

func TestLeituraDeCampoInteiro(t *testing.T) {
	resp := []byte(`{"accepted":18,"duplicates":2,"rejected":0,"cursor":91841}`)
	for _, c := range []struct {
		campo string
		want  int64
	}{{"accepted", 18}, {"duplicates", 2}, {"rejected", 0}, {"cursor", 91841}} {
		got, ok := campoInt(resp, c.campo)
		if !ok || got != c.want {
			t.Errorf("%s = %d (ok=%v), esperado %d", c.campo, got, ok, c.want)
		}
	}
	if _, ok := campoInt(resp, "inexistente"); ok {
		t.Error("campo inexistente foi encontrado")
	}
}

// ---- comportamento do cliente ----

type servidorFalso struct {
	*httptest.Server
	chamadas  atomic.Int32
	status    atomic.Int32
	accepted  int
	dupes     int
	cursor    int64
	ultimoTok atomic.Value
	ultimoLot atomic.Value
}

func novoServidor() *servidorFalso {
	s := &servidorFalso{}
	s.status.Store(200)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.chamadas.Add(1)
		s.ultimoTok.Store(r.Header.Get("Authorization"))
		b, _ := io.ReadAll(r.Body)
		s.ultimoLot.Store(string(b))

		st := int(s.status.Load())
		if st != 200 {
			w.WriteHeader(st)
			_, _ = w.Write([]byte(`{"error":{"code":"x","message":"y"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":` + itoa(s.accepted) +
			`,"duplicates":` + itoa(s.dupes) +
			`,"rejected":0,"cursor":` + itoa(int(s.cursor)) + `}`))
	}))
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func novoCliente(t *testing.T, url string) *Client {
	t.Helper()
	return New(Credenciais{
		BaseURL: url,
		PetID:   "11111111-1111-1111-1111-111111111111",
		Token:   "token-de-teste",
	}, Opcoes{Espera: func(time.Duration) {}})
}

func TestSyncMandaOBearerEOLote(t *testing.T) {
	srv := novoServidor()
	defer srv.Close()
	srv.accepted = 3
	c := novoCliente(t, srv.URL)

	n, err := c.Sync(evs(3))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("enviados = %d, esperado 3", n)
	}
	if tok, _ := srv.ultimoTok.Load().(string); tok != "Bearer token-de-teste" {
		t.Errorf("Authorization = %q", tok)
	}
	if lote, _ := srv.ultimoLot.Load().(string); !strings.Contains(lote, `"kind":"effort"`) {
		t.Errorf("o lote não tem o kind esperado: %s", lote)
	}
}

// DUPLICADO CONTA COMO ENVIADO. O servidor já tem esse evento, então ele pode
// sair da fila local. Se só o accepted contasse, um lote reenviado depois de
// um ACK perdido ficaria preso na fila pra sempre, e a fila encheria.
func TestDuplicadoContaComoEnviado(t *testing.T) {
	srv := novoServidor()
	defer srv.Close()
	srv.accepted, srv.dupes = 1, 4
	c := novoCliente(t, srv.URL)

	n, err := c.Sync(evs(5))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("enviados = %d, esperado 5 (1 aceito + 4 duplicados)", n)
	}
}

// O api-contract limita o lote a 200. Mandar mais leva 400 e o device ficaria
// travado: cortar aqui é o que faz uma fila grande drenar em vez de emperrar.
func TestLoteGrandeEhCortadoEm200(t *testing.T) {
	srv := novoServidor()
	defer srv.Close()
	srv.accepted = 200
	c := novoCliente(t, srv.URL)

	n, err := c.Sync(evs(250))
	if err != nil {
		t.Fatal(err)
	}
	if n != 200 {
		t.Errorf("enviados = %d, esperado 200", n)
	}
	lote, _ := srv.ultimoLot.Load().(string)
	if got := strings.Count(lote, `"id":`); got != 200 {
		t.Errorf("o corpo levou %d eventos, esperado 200", got)
	}
}

// Erro transitório do servidor merece retry: todo POST daqui é idempotente por
// ULID, então reenviar é seguro.
func TestErro5xxTentaDeNovo(t *testing.T) {
	srv := novoServidor()
	defer srv.Close()
	srv.status.Store(503)
	c := novoCliente(t, srv.URL)

	if _, err := c.Sync(evs(2)); err == nil {
		t.Fatal("503 repetido devolveu sucesso")
	}
	if got := srv.chamadas.Load(); got != 3 {
		t.Errorf("%d tentativas, esperado 3", got)
	}
}

// 4xx é erro do cliente: insistir só gasta bateria e rádio.
func TestErro4xxNaoTentaDeNovo(t *testing.T) {
	for _, st := range []int32{400, 401, 404} {
		srv := novoServidor()
		srv.status.Store(st)
		c := novoCliente(t, srv.URL)

		if _, err := c.Sync(evs(2)); err == nil {
			t.Errorf("status %d devolveu sucesso", st)
		}
		if got := srv.chamadas.Load(); got != 1 {
			t.Errorf("status %d gerou %d tentativas, esperado 1", st, got)
		}
		srv.Close()
	}
}

func TestSyncDeLoteVazioNaoBateNaRede(t *testing.T) {
	srv := novoServidor()
	defer srv.Close()
	c := novoCliente(t, srv.URL)

	n, err := c.Sync(nil)
	if err != nil || n != 0 {
		t.Errorf("n=%d err=%v, esperado 0 e nil", n, err)
	}
	if srv.chamadas.Load() != 0 {
		t.Error("lote vazio ligou o rádio à toa")
	}
}

// Sem rede o device não pode travar: ele é autoridade local e tenta de novo
// no próximo intervalo.
func TestServidorInalcancavelDaErroSemTravar(t *testing.T) {
	c := New(Credenciais{
		BaseURL: "http://127.0.0.1:1", PetID: "p", Token: "t",
	}, Opcoes{Espera: func(time.Duration) {}})

	pronto := make(chan struct{})
	go func() {
		defer close(pronto)
		if _, err := c.Sync(evs(1)); err == nil {
			t.Error("servidor inalcançável devolveu sucesso")
		}
	}()
	select {
	case <-pronto:
	case <-time.After(10 * time.Second):
		t.Fatal("o Sync travou com o servidor inalcançável")
	}
}

func TestLeituraDeCampoTexto(t *testing.T) {
	resp := []byte(`{"device_id":"d-1","pet_id":"p-2","token":"seg\"redo"}`)
	for _, c := range []struct{ campo, want string }{
		{"device_id", "d-1"}, {"pet_id", "p-2"}, {"token", `seg"redo`},
	} {
		got, ok := campoTexto(resp, c.campo)
		if !ok || got != c.want {
			t.Errorf("%s = %q (ok=%v), esperado %q", c.campo, got, ok, c.want)
		}
	}
	if _, ok := campoTexto(resp, "inexistente"); ok {
		t.Error("campo inexistente foi encontrado")
	}
}

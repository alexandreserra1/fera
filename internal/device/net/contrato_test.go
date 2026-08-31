package net

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpapi "github.com/ale/fera/internal/http"
	"github.com/ale/fera/internal/service"
	"github.com/ale/fera/internal/sig"
	"github.com/ale/fera/internal/sim"
)

// Este é o teste que fecha o ciclo: o JSON escrito à mão do device passa pelo
// ROUTER DE VERDADE do servidor, com a validação de borda, a autenticação e a
// autorização por pet no caminho.
//
// Sem ele, as duas pontas teriam cada uma o próprio teste passando e ninguém
// saberia que discordam do formato até um device em campo tomar 400.

const petDoTeste = "11111111-1111-1111-1111-111111111111"

// Implementa a interface (não exportada) que o httpapi.NewPetHandler consome.
// Roda o sim de verdade, então o estado do outro lado é o estado real.
type servicoReal struct {
	log []sim.Event
}

func (s *servicoReal) Ingest(_ context.Context, _ string, evs []sim.Event) (service.IngestResult, error) {
	visto := map[string]bool{}
	for _, e := range s.log {
		visto[e.ID] = true
	}
	novos := 0
	for _, e := range evs {
		if visto[e.ID] {
			continue
		}
		visto[e.ID] = true
		s.log = append(s.log, e)
		novos++
	}
	return service.IngestResult{
		Accepted: novos, Duplicates: len(evs) - novos, Cursor: int64(len(s.log)),
	}, nil
}

func (s *servicoReal) Get(_ context.Context, petID string) (sim.View, error) {
	t := sim.DefaultTuning()
	if len(s.log) == 0 {
		return sim.Project(sim.Genesis(petID, t0), t0, t), nil
	}
	return sim.Project(sim.Fold(sim.Genesis(petID, s.log[0].At), s.log, t), t0.Add(24*time.Hour), t), nil
}

func (s *servicoReal) Events(_ context.Context, _ string, cursor int64, limit int) ([]sim.Event, int64, error) {
	if cursor >= int64(len(s.log)) {
		return nil, cursor, nil
	}
	fim := cursor + int64(limit)
	if fim > int64(len(s.log)) {
		fim = int64(len(s.log))
	}
	return s.log[cursor:fim], fim, nil
}

type authReal struct {
	token    string
	deviceID string
	agora    func() time.Time
}

func (a authReal) Authenticate(_ context.Context, tok string) (string, string, error) {
	if tok != a.token {
		return "", "", service.ErrForbidden
	}
	return a.deviceID, petDoTeste, nil
}

// Usa o sig de VERDADE, o mesmo pacote que o device usa pra assinar. Se as
// duas pontas discordarem da string canônica, isto falha aqui e não em campo.
func (a authReal) AuthenticateSigned(_ context.Context, req service.Assinada) (string, error) {
	if req.DeviceID != a.deviceID {
		return "", service.ErrForbidden
	}
	if !sig.DentroDaJanela(req.Quando, a.agora(), sig.JanelaPadrao) {
		return "", service.ErrForbidden
	}
	if !sig.Confere(sig.Chave(a.token), req.Metodo, req.Caminho, req.Quando, req.Corpo, req.Assinatura) {
		return "", service.ErrForbidden
	}
	return petDoTeste, nil
}

type devsReal struct{}

func (devsReal) Register(context.Context) (service.Registration, error) {
	return service.Registration{DeviceID: "dev-1", PetID: petDoTeste, Token: "token-bom"}, nil
}
func (devsReal) Authenticate(context.Context, string) (string, string, error) {
	return "", "", service.ErrForbidden
}

func servidorReal(t *testing.T, token string) (*httptest.Server, *servicoReal) {
	t.Helper()
	svc := &servicoReal{}
	agora := func() time.Time { return t0.Add(24 * time.Hour) }
	srv := httptest.NewServer(httpapi.NewRouter(
		httpapi.NewPetHandler(svc, agora),
		httpapi.NewDeviceHandler(devsReal{}),
		authReal{token: token, deviceID: "dev-1", agora: agora},
		agora,
		nil,
	))
	t.Cleanup(srv.Close)
	return srv, svc
}

// credsAssinando devolve credenciais que fazem o cliente ASSINAR em vez de
// mandar Bearer.
func credsAssinando(url, token string) Credenciais {
	return Credenciais{BaseURL: url, PetID: petDoTeste, Token: token, DeviceID: "dev-1"}
}

func opcAssinando() Opcoes {
	return Opcoes{
		Espera: func(time.Duration) {},
		Agora:  func() time.Time { return t0.Add(24 * time.Hour) },
	}
}

func TestOJSONDoDeviceEhAceitoPeloServidorDeVerdade(t *testing.T) {
	srv, svc := servidorReal(t, "token-bom")
	c := New(Credenciais{BaseURL: srv.URL, PetID: petDoTeste, Token: "token-bom"},
		Opcoes{Espera: func(time.Duration) {}})

	// um evento de cada Kind, pra que nenhum fique sem cobertura de formato
	lote := []sim.Event{
		{ID: idDeTeste(0), At: t0.Add(time.Hour), Kind: sim.KindEffort, Kcal: 620, Zone: 4},
		{ID: idDeTeste(1), At: t0.Add(2 * time.Hour), Kind: sim.KindSleep, Minutes: 430},
		{ID: idDeTeste(2), At: t0.Add(3 * time.Hour), Kind: sim.KindInteract},
		{ID: idDeTeste(3), At: t0.Add(4 * time.Hour), Kind: sim.KindEncounter, PeerID: idDeTeste(9)},
	}

	n, err := c.Sync(lote)
	if err != nil {
		t.Fatalf("o servidor recusou o lote do device: %v", err)
	}
	if n != len(lote) {
		t.Fatalf("enviados = %d, esperado %d", n, len(lote))
	}

	// O servidor tem que ter reconstruído EXATAMENTE os mesmos eventos:
	// se algum campo se perder no wire, o fold dos dois lados diverge.
	if len(svc.log) != len(lote) {
		t.Fatalf("o servidor guardou %d eventos, esperado %d", len(svc.log), len(lote))
	}
	for i := range lote {
		if svc.log[i] != lote[i] {
			t.Errorf("evento %d chegou diferente\n  no servidor: %+v\n  no device:   %+v",
				i, svc.log[i], lote[i])
		}
	}
}

// A prova final do "um core, três alvos": device e servidor foldam os MESMOS
// eventos e chegam no MESMO estado, tendo passado por HTTP no meio.
func TestDeviceEServidorConvergemDepoisDoSync(t *testing.T) {
	srv, svc := servidorReal(t, "token-bom")
	c := New(Credenciais{BaseURL: srv.URL, PetID: petDoTeste, Token: "token-bom"},
		Opcoes{Espera: func(time.Duration) {}})

	lote := []sim.Event{
		{ID: idDeTeste(0), At: t0.Add(time.Hour), Kind: sim.KindEffort, Kcal: 620, Zone: 4},
		{ID: idDeTeste(1), At: t0.Add(9 * time.Hour), Kind: sim.KindSleep, Minutes: 430},
		{ID: idDeTeste(2), At: t0.Add(11 * time.Hour), Kind: sim.KindInteract},
	}
	if _, err := c.Sync(lote); err != nil {
		t.Fatal(err)
	}

	tn := sim.DefaultTuning()
	agora := t0.Add(24 * time.Hour)
	noDevice := sim.Project(sim.Fold(sim.Genesis(petDoTeste, lote[0].At), lote, tn), agora, tn)

	noServidor, err := svc.Get(context.Background(), petDoTeste)
	if err != nil {
		t.Fatal(err)
	}
	if noDevice != noServidor {
		t.Errorf("device e servidor divergiram depois do sync\n device: %+v\nservidor: %+v",
			noDevice, noServidor)
	}
}

// Reenviar o lote inteiro é o caminho feliz de um ACK perdido. O servidor
// responde 200 com duplicates, e o device tem que poder tirar tudo da fila.
func TestReenvioDrenaAFilaEmVezDeEmperrar(t *testing.T) {
	srv, _ := servidorReal(t, "token-bom")
	c := New(Credenciais{BaseURL: srv.URL, PetID: petDoTeste, Token: "token-bom"},
		Opcoes{Espera: func(time.Duration) {}})

	lote := []sim.Event{
		{ID: idDeTeste(0), At: t0.Add(time.Hour), Kind: sim.KindEffort, Kcal: 620, Zone: 4},
		{ID: idDeTeste(1), At: t0.Add(2 * time.Hour), Kind: sim.KindInteract},
	}
	if _, err := c.Sync(lote); err != nil {
		t.Fatal(err)
	}
	n, err := c.Sync(lote)
	if err != nil {
		t.Fatalf("o reenvio deu erro em vez de 200: %v", err)
	}
	if n != len(lote) {
		t.Errorf("o reenvio devolveu %d enviados, esperado %d: a fila do device "+
			"nunca esvaziaria", n, len(lote))
	}
}

// Token errado tem que virar erro permanente, sem retry: insistir com
// credencial ruim só gasta bateria e rádio.
func TestTokenRuimEhPermanente(t *testing.T) {
	srv, _ := servidorReal(t, "token-bom")
	c := New(Credenciais{BaseURL: srv.URL, PetID: petDoTeste, Token: "token-ruim"},
		Opcoes{Espera: func(time.Duration) { t.Error("houve retry com token ruim") }})

	_, err := c.Sync([]sim.Event{{ID: idDeTeste(0), At: t0, Kind: sim.KindInteract}})
	if err == nil {
		t.Fatal("token ruim passou")
	}
}

// O pet de outro dono é 404 pro device, e 404 também é permanente.
func TestPetDeOutroNaoRendeRetry(t *testing.T) {
	srv, _ := servidorReal(t, "token-bom")
	c := New(Credenciais{
		BaseURL: srv.URL,
		PetID:   "99999999-9999-9999-9999-999999999999",
		Token:   "token-bom",
	}, Opcoes{Espera: func(time.Duration) { t.Error("houve retry num 404") }})

	if _, err := c.Sync([]sim.Event{{ID: idDeTeste(0), At: t0, Kind: sim.KindInteract}}); err == nil {
		t.Fatal("escrever no pet de outro passou")
	}
}

func idDeTeste(i int) string {
	b := []byte("01JAAAAAAAAAAAAAAAAAAAAAAA")
	b[len(b)-1] = byte('A' + i)
	return string(b)
}

// Registrar é o único caminho sem credencial, e é por onde ela nasce. Testado
// contra o endpoint de verdade porque o formato da resposta é contrato.
func TestRegistrarPegaCredencialDoServidorReal(t *testing.T) {
	srv, _ := servidorReal(t, "irrelevante")

	creds, err := Registrar(srv.URL, Opcoes{Espera: func(time.Duration) {}})
	if err != nil {
		t.Fatal(err)
	}
	if creds.PetID != petDoTeste {
		t.Errorf("pet_id = %q, esperado %q", creds.PetID, petDoTeste)
	}
	if creds.Token == "" {
		t.Error("o registro veio sem token")
	}
	if creds.BaseURL != srv.URL {
		t.Errorf("base_url = %q, esperado %q", creds.BaseURL, srv.URL)
	}
}

// O teste central desta fatia: o device assina, o SEGREDO NÃO VAI NO FIO, e o
// servidor de verdade aceita.
func TestDeviceAssinaEOServidorAceitaSemOTokenNoFio(t *testing.T) {
	srv, svc := servidorReal(t, "token-bom")

	var visto struct {
		auth      string
		deviceHdr string
		assinado  string
		corpo     string
	}
	espiao := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visto.auth = r.Header.Get("Authorization")
		visto.deviceHdr = r.Header.Get(sig.HeaderDevice)
		visto.assinado = r.Header.Get(sig.HeaderAssinatura)
		b, _ := io.ReadAll(r.Body)
		visto.corpo = string(b)
		r.Body = io.NopCloser(strings.NewReader(visto.corpo))
		// repassa pro servidor de verdade
		req, _ := http.NewRequest(r.Method, srv.URL+r.URL.RequestURI(), strings.NewReader(visto.corpo))
		req.Header = r.Header.Clone()
		resp, err := srv.Client().Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(rb)
	}))
	defer espiao.Close()

	c := New(credsAssinando(espiao.URL, "token-bom"), opcAssinando())
	lote := []sim.Event{{ID: idDeTeste(0), At: t0.Add(time.Hour), Kind: sim.KindEffort, Kcal: 620, Zone: 4}}

	n, err := c.Sync(lote)
	if err != nil {
		t.Fatalf("o servidor recusou a requisição assinada: %v", err)
	}
	if n != 1 {
		t.Errorf("enviados = %d, esperado 1", n)
	}
	if visto.auth != "" {
		t.Errorf("o header Authorization foi enviado: %q", visto.auth)
	}
	if strings.Contains(visto.corpo, "token-bom") || strings.Contains(visto.assinado, "token-bom") {
		t.Error("o token apareceu no fio")
	}
	if visto.deviceHdr != "dev-1" || len(visto.assinado) != 64 {
		t.Errorf("headers de assinatura errados: device=%q sig=%q", visto.deviceHdr, visto.assinado)
	}
	if len(svc.log) != 1 {
		t.Errorf("o servidor guardou %d eventos, esperado 1", len(svc.log))
	}
}

// Adulterar o corpo em trânsito tem que derrubar a assinatura. É isso que a
// assinatura compra em cima do HTTP puro.
func TestCorpoAdulteradoEmTransitoEhRecusado(t *testing.T) {
	srv, _ := servidorReal(t, "token-bom")

	// atacante no meio: troca 620 kcal por 9999
	atacante := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		adulterado := strings.Replace(string(b), `"kcal":620`, `"kcal":9999`, 1)
		req, _ := http.NewRequest(r.Method, srv.URL+r.URL.RequestURI(), strings.NewReader(adulterado))
		req.Header = r.Header.Clone()
		resp, err := srv.Client().Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		rb, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(rb)
	}))
	defer atacante.Close()

	c := New(credsAssinando(atacante.URL, "token-bom"), opcAssinando())
	lote := []sim.Event{{ID: idDeTeste(0), At: t0.Add(time.Hour), Kind: sim.KindEffort, Kcal: 620, Zone: 4}}

	if _, err := c.Sync(lote); err == nil {
		t.Fatal("o corpo adulterado foi aceito: a assinatura não está protegendo o conteúdo")
	}
}

// Relógio fora da janela é recusado, e a recusa carrega a hora do servidor
// pra que um device sem RTC possa se corrigir em vez de ficar preso.
func TestRelogioForaDaJanelaRecebeAHoraDoServidor(t *testing.T) {
	srv, _ := servidorReal(t, "token-bom")

	opc := opcAssinando()
	opc.Agora = func() time.Time { return t0.Add(-90 * 24 * time.Hour) } // device perdido no tempo
	c := New(credsAssinando(srv.URL, "token-bom"), opc)

	if _, err := c.Sync([]sim.Event{{ID: idDeTeste(0), At: t0, Kind: sim.KindInteract}}); err == nil {
		t.Fatal("relógio fora da janela passou")
	}

	// e a pista está no header
	req, _ := http.NewRequest("GET", srv.URL+"/v1/pets/"+petDoTeste, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if h := resp.Header.Get(sig.HeaderHora); h == "" {
		t.Error("a recusa não trouxe a hora do servidor")
	}
}

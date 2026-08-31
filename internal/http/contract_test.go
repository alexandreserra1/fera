package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ale/fera/internal/service"
	"github.com/ale/fera/internal/sig"
	"github.com/ale/fera/internal/sim"
)

// agora é fixo pra que "90 dias no passado" e "24h no futuro" sejam
// determinísticos. Teste que depende do relógio de verdade falha sozinho
// daqui a três meses.
var agora = time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

// fakeSvc é o service em memória. Roda o sim DE VERDADE, então os números de
// GET nos arquivos de contrato são o estado real do bicho, não invenção.
type fakeSvc struct {
	mu  sync.Mutex
	log []sim.Event
	err error
}

func (f *fakeSvc) Ingest(_ context.Context, _ string, evs []sim.Event) (service.IngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return service.IngestResult{}, f.err
	}
	visto := map[string]bool{}
	for _, e := range f.log {
		visto[e.ID] = true
	}
	aceitos := 0
	for _, e := range evs {
		if visto[e.ID] {
			continue
		}
		visto[e.ID] = true
		f.log = append(f.log, e)
		aceitos++
	}
	return service.IngestResult{
		Accepted:   aceitos,
		Duplicates: len(evs) - aceitos,
		Cursor:     int64(len(f.log)),
	}, nil
}

func (f *fakeSvc) Events(_ context.Context, _ string, cursor int64, limit int) ([]sim.Event, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, 0, f.err
	}
	if cursor >= int64(len(f.log)) {
		return nil, cursor, nil
	}
	fim := cursor + int64(limit)
	if fim > int64(len(f.log)) {
		fim = int64(len(f.log))
	}
	return append([]sim.Event{}, f.log[cursor:fim]...), fim, nil
}

func (f *fakeSvc) Get(_ context.Context, petID string) (sim.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return sim.View{}, f.err
	}
	t := sim.DefaultTuning()
	if len(f.log) == 0 {
		return sim.Project(sim.Genesis(petID, agora), agora, t), nil
	}
	primeiro := f.log[0].At
	for _, e := range f.log[1:] {
		if e.At.Before(primeiro) {
			primeiro = e.At
		}
	}
	return sim.Project(sim.Fold(sim.Genesis(petID, primeiro), f.log, t), agora, t), nil
}

// fakeAuth resolve um token conhecido pro pet dos arquivos de contrato.
// Qualquer outro token é ErrForbidden, que é o que o repo de verdade devolve
// pra hash não encontrado.
type fakeAuth struct {
	token string
	petID string
}

func (f fakeAuth) Authenticate(_ context.Context, token string) (string, string, error) {
	if token != f.token {
		return "", "", service.ErrForbidden
	}
	return "device-1", f.petID, nil
}

func (f fakeAuth) AuthenticateSigned(_ context.Context, req service.Assinada) (string, error) {
	if !sig.Confere(sig.Chave(f.token), req.Metodo, req.Caminho, req.Quando, req.Corpo, req.Assinatura) {
		return "", service.ErrForbidden
	}
	return f.petID, nil
}

type fakeDevices struct{ reg service.Registration }

func (f fakeDevices) Register(context.Context) (service.Registration, error) { return f.reg, nil }
func (f fakeDevices) Authenticate(context.Context, string) (string, string, error) {
	return "", "", service.ErrForbidden
}

const (
	petDoContrato   = "11111111-1111-1111-1111-111111111111"
	tokenDoContrato = "token-valido-do-contrato"
)

type caso struct {
	Name     string          `json:"name"`
	NoAuth   bool            `json:"no_auth"`
	Token    string          `json:"token"`
	Seed     []eventDTO      `json:"seed"`
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Body     json.RawMessage `json:"body"`
	RawBody  string          `json:"raw_body"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response"`
}

// Os arquivos em testdata/ são documentação executável do api-contract. Um
// arquivo por comportamento, disparado contra um httptest.Server de verdade.
// Se alguém mudar o formato da resposta sem querer, o CI vê antes do device.
func TestContrato(t *testing.T) {
	arquivos, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(arquivos) == 0 {
		t.Fatal("nenhum arquivo de contrato em testdata/")
	}

	for _, arq := range arquivos {
		t.Run(filepath.Base(arq), func(t *testing.T) {
			b, err := os.ReadFile(arq)
			if err != nil {
				t.Fatal(err)
			}
			var c caso
			if err := json.Unmarshal(b, &c); err != nil {
				t.Fatal(err)
			}

			svc := &fakeSvc{}
			if len(c.Seed) > 0 {
				evs, rej := ingestRequest{Events: c.Seed}.toDomain(agora)
				if len(rej) > 0 {
					t.Fatalf("o seed do próprio arquivo tem evento inválido: %v", rej)
				}
				if _, err := svc.Ingest(context.Background(), "", evs); err != nil {
					t.Fatal(err)
				}
			}

			srv := httptest.NewServer(novoRouter(svc))
			defer srv.Close()

			var corpo io.Reader
			switch {
			case c.RawBody != "":
				corpo = bytes.NewBufferString(c.RawBody)
			case len(c.Body) > 0:
				corpo = bytes.NewReader(c.Body)
			}

			req, err := http.NewRequest(c.Method, srv.URL+c.Path, corpo)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			if !c.NoAuth {
				token := c.Token
				if token == "" {
					token = tokenDoContrato
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}

			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			lido, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}

			if resp.StatusCode != c.Status {
				t.Errorf("status = %d, esperado %d\ncorpo: %s", resp.StatusCode, c.Status, lido)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, esperado application/json", ct)
			}

			// compara a estrutura, não os bytes: indentação e ordem de chave
			// não fazem parte do contrato
			var got, want any
			if err := json.Unmarshal(lido, &got); err != nil {
				t.Fatalf("resposta não é JSON: %s", lido)
			}
			if err := json.Unmarshal(c.Response, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("corpo divergiu do contrato\n got: %s\nwant: %s", lido, c.Response)
			}
		})
	}
}

// Erro não classificado do service nunca pode virar corpo de resposta: é assim
// que nome de tabela e mensagem de driver vazam pro cliente.
func TestErroInternoNaoVazaDetalhe(t *testing.T) {
	svc := &fakeSvc{err: errDeBanco{}}
	srv := httptest.NewServer(novoRouter(svc))
	defer srv.Close()

	resp, err := autenticado(t, srv, "GET", "/v1/pets/"+petDoContrato)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, esperado 500", resp.StatusCode)
	}
	if bytes.Contains(b, []byte("pet_snapshots")) || bytes.Contains(b, []byte("SQLSTATE")) {
		t.Errorf("detalhe interno vazou na resposta: %s", b)
	}
	var body errBody
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("resposta de erro fora do formato: %s", b)
	}
	if body.Error.Code != codeInternal {
		t.Errorf("code = %q, esperado %q", body.Error.Code, codeInternal)
	}
}

type errDeBanco struct{}

func (errDeBanco) Error() string {
	return `ERROR: relation "pet_snapshots" does not exist (SQLSTATE 42P01)`
}

// As sentinelas do service têm que virar status HTTP, e é errors.Is que faz
// isso: o erro chega embrulhado em %w e comparação direta falharia.
func TestSentinelaDoServiceViraStatus(t *testing.T) {
	svc := &fakeSvc{err: fmt.Errorf("carregar pet: %w", service.ErrNotFound)}
	srv := httptest.NewServer(novoRouter(svc))
	defer srv.Close()

	resp, err := autenticado(t, srv, "GET", "/v1/pets/"+petDoContrato)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, esperado 404", resp.StatusCode)
	}
}

func novoRouter(svc *fakeSvc) http.Handler {
	return NewRouter(
		NewPetHandler(svc, func() time.Time { return agora }),
		NewDeviceHandler(fakeDevices{reg: service.Registration{
			DeviceID: "dev-1", PetID: petDoContrato, Token: "segredo",
		}}),
		fakeAuth{token: tokenDoContrato, petID: petDoContrato},
		func() time.Time { return agora },
		nil,
		Limite{}, // desligado: estes testes exercitam contrato, não cota
	)
}

func autenticado(t *testing.T, srv *httptest.Server, metodo, caminho string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(metodo, srv.URL+caminho, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tokenDoContrato)
	return srv.Client().Do(req)
}

// O caminho assinado tem que funcionar pelo router de verdade, e o Bearer tem
// que continuar funcionando: o app usa um, o device usa o outro.
func TestRouterAceitaAssinaturaEBearer(t *testing.T) {
	svc := &fakeSvc{}
	srv := httptest.NewServer(novoRouter(svc))
	defer srv.Close()

	caminho := "/v1/pets/" + petDoContrato

	// Bearer
	req, _ := http.NewRequest("GET", srv.URL+caminho, nil)
	req.Header.Set("Authorization", "Bearer "+tokenDoContrato)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Bearer devolveu %d", resp.StatusCode)
	}

	// assinatura
	req2, _ := http.NewRequest("GET", srv.URL+caminho, nil)
	req2.Header.Set(sig.HeaderDevice, "device-1")
	req2.Header.Set(sig.HeaderTimestamp, strconv.FormatInt(agora.Unix(), 10))
	req2.Header.Set(sig.HeaderAssinatura, sig.Assinar(sig.Chave(tokenDoContrato), "GET", caminho, agora, nil))
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("assinatura devolveu %d", resp2.StatusCode)
	}
}

// A query entra na assinatura: sem isso, dava pra trocar o ?since= de um pull
// em trânsito e fazer o device reprocessar ou pular eventos.
func TestQueryEntraNaAssinatura(t *testing.T) {
	svc := &fakeSvc{}
	srv := httptest.NewServer(novoRouter(svc))
	defer srv.Close()

	assinado := "/v1/pets/" + petDoContrato + "/events?since=0"
	enviado := "/v1/pets/" + petDoContrato + "/events?since=999"

	req, _ := http.NewRequest("GET", srv.URL+enviado, nil)
	req.Header.Set(sig.HeaderDevice, "device-1")
	req.Header.Set(sig.HeaderTimestamp, strconv.FormatInt(agora.Unix(), 10))
	req.Header.Set(sig.HeaderAssinatura, sig.Assinar(sig.Chave(tokenDoContrato), "GET", assinado, agora, nil))

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, esperado 401: a query trocada passou", resp.StatusCode)
	}
}

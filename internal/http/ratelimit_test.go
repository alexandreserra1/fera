package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ale/fera/internal/sig"
)

// pedir faz uma requisição autenticada como o device dado.
func pedir(t *testing.T, srv *httptest.Server, deviceID, petID string) int {
	t.Helper()
	caminho := "/v1/pets/" + petID
	req, err := http.NewRequest("GET", srv.URL+caminho, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(sig.HeaderDevice, deviceID)
	req.Header.Set(sig.HeaderTimestamp, strconv.FormatInt(agora.Unix(), 10))
	req.Header.Set(sig.HeaderAssinatura,
		sig.Assinar(sig.Chave(tokenDoContrato), "GET", caminho, agora, nil))

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestTrafegoNormalPassa(t *testing.T) {
	srv := httptest.NewServer(novoRouter(&fakeSvc{}))
	defer srv.Close()

	for i := range 10 {
		if st := pedir(t, srv, "device-1", petDoContrato); st != 200 {
			t.Fatalf("requisição %d devolveu %d: tráfego normal foi barrado", i, st)
		}
	}
}

// Rajada acima do teto vira 429, não 500 nem erro de banco: o limite existe
// pra proteger o servidor de um device com retry maluco, e o device precisa
// entender que deve esperar.
func TestRajadaAcimaDoTetoVira429(t *testing.T) {
	svc := &fakeSvc{}
	srv := httptest.NewServer(NewRouter(
		NewPetHandler(svc, func() time.Time { return agora }),
		NewDeviceHandler(fakeDevices{}),
		fakeAuth{token: tokenDoContrato, petID: petDoContrato},
		func() time.Time { return agora },
		nil,
		Limite{PorMinuto: 60, Rajada: 5},
	))
	defer srv.Close()

	var barradas int
	for range 20 {
		if pedir(t, srv, "device-1", petDoContrato) == http.StatusTooManyRequests {
			barradas++
		}
	}
	if barradas == 0 {
		t.Error("20 requisições com rajada 5 não barraram nenhuma")
	}
	if barradas == 20 {
		t.Error("barrou todas: nem a rajada inicial passou")
	}
}

// O limite é POR DEVICE. Um device abusado não pode derrubar o bicho de outro
// dono, que é o que aconteceria com um limite global.
func TestLimiteEhPorDevice(t *testing.T) {
	svc := &fakeSvc{}
	srv := httptest.NewServer(NewRouter(
		NewPetHandler(svc, func() time.Time { return agora }),
		NewDeviceHandler(fakeDevices{}),
		fakeAuth{token: tokenDoContrato, petID: petDoContrato},
		func() time.Time { return agora },
		nil,
		Limite{PorMinuto: 60, Rajada: 3},
	))
	defer srv.Close()

	// device-1 esgota a cota dele
	for range 10 {
		pedir(t, srv, "device-1", petDoContrato)
	}
	if st := pedir(t, srv, "device-1", petDoContrato); st != http.StatusTooManyRequests {
		t.Fatalf("device-1 devolveu %d depois de estourar, esperado 429", st)
	}

	// device-2 tem que continuar passando
	if st := pedir(t, srv, "device-2", petDoContrato); st != 200 {
		t.Errorf("device-2 devolveu %d: o limite de outro device vazou pra ele", st)
	}
}

// 429 tem que sair no formato de erro único do api-contract, com Retry-After
// pra que o device saiba quanto esperar em vez de martelar.
func TestOErroDe429SegueOContrato(t *testing.T) {
	svc := &fakeSvc{}
	srv := httptest.NewServer(NewRouter(
		NewPetHandler(svc, func() time.Time { return agora }),
		NewDeviceHandler(fakeDevices{}),
		fakeAuth{token: tokenDoContrato, petID: petDoContrato},
		func() time.Time { return agora },
		nil,
		Limite{PorMinuto: 60, Rajada: 1},
	))
	defer srv.Close()

	caminho := "/v1/pets/" + petDoContrato
	var resp *http.Response
	for range 5 {
		req, _ := http.NewRequest("GET", srv.URL+caminho, nil)
		req.Header.Set(sig.HeaderDevice, "device-1")
		req.Header.Set(sig.HeaderTimestamp, strconv.FormatInt(agora.Unix(), 10))
		req.Header.Set(sig.HeaderAssinatura,
			sig.Assinar(sig.Chave(tokenDoContrato), "GET", caminho, agora, nil))
		r, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if r.StatusCode == http.StatusTooManyRequests {
			resp = r
			break
		}
		r.Body.Close()
	}
	if resp == nil {
		t.Fatal("não consegui provocar um 429")
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var corpo errBody
	if err := json.Unmarshal(b, &corpo); err != nil {
		t.Fatalf("429 fora do formato de erro: %s", b)
	}
	if corpo.Error.Code != codeRateLimit {
		t.Errorf("code = %q, esperado %q", corpo.Error.Code, codeRateLimit)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 sem Retry-After: o device não sabe quanto esperar")
	}
}

// Limite zero desliga o middleware. É o que os testes de contrato usam, e o
// que uma instalação caseira sem exposição na rede quer.
func TestLimiteZeroDesliga(t *testing.T) {
	srv := httptest.NewServer(novoRouter(&fakeSvc{}))
	defer srv.Close()
	for range 300 {
		if st := pedir(t, srv, "device-1", petDoContrato); st != 200 {
			t.Fatalf("com limite desligado alguma requisição devolveu %d", st)
		}
	}
}

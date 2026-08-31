package net

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// O cliente HTTP/1.1 é escrito à mão porque net/http linka crypto/tls
// incondicionalmente: 415 KB de flash e 189 KB de RAM contra 89 e 40 (ver
// docs/06). Estes testes rodam contra httptest.Server, que fala HTTP de
// verdade, então o mesmo código exercitado aqui é o que roda na placa.

func servidorEco(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestFazRequisicaoEEntendeAResposta(t *testing.T) {
	var visto struct{ metodo, caminho, corpo, host string }
	addr := servidorEco(t, func(w http.ResponseWriter, r *http.Request) {
		visto.metodo, visto.caminho, visto.host = r.Method, r.URL.RequestURI(), r.Host
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		visto.corpo = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":3,"duplicates":1,"rejected":0,"cursor":42}`))
	})

	c := &http1{addr: addr, timeout: 5 * time.Second}
	resp, err := c.fazer("POST", "/v1/pets/x/events", []byte(`{"events":[]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.status != 200 {
		t.Errorf("status = %d, esperado 200", resp.status)
	}
	if visto.metodo != "POST" || visto.caminho != "/v1/pets/x/events" {
		t.Errorf("o servidor viu %s %s", visto.metodo, visto.caminho)
	}
	if visto.corpo != `{"events":[]}` {
		t.Errorf("corpo = %q", visto.corpo)
	}
	if visto.host == "" {
		t.Error("Host vazio: HTTP/1.1 exige o header Host")
	}
	if n, ok := campoInt(resp.corpo, "cursor"); !ok || n != 42 {
		t.Errorf("corpo da resposta = %q", resp.corpo)
	}
}

func TestMandaOsHeadersPedidos(t *testing.T) {
	var vistos http.Header
	addr := servidorEco(t, func(w http.ResponseWriter, r *http.Request) {
		vistos = r.Header.Clone()
		_, _ = w.Write([]byte("{}"))
	})

	c := &http1{addr: addr, timeout: 5 * time.Second}
	_, err := c.fazer("POST", "/x", []byte("{}"), []cabecalho{
		{"X-Fera-Device", "dev-1"},
		{"X-Fera-Signature", "abc123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if vistos.Get("X-Fera-Device") != "dev-1" || vistos.Get("X-Fera-Signature") != "abc123" {
		t.Errorf("headers chegaram errados: %v", vistos)
	}
	if vistos.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", vistos.Get("Content-Type"))
	}
}

// Status diferente de 200 tem que chegar ao chamador com o corpo: é assim que
// o Sync distingue 4xx (permanente) de 5xx (tentar de novo).
func TestStatusDeErroChegaComOCorpo(t *testing.T) {
	for _, st := range []int{400, 401, 404, 500, 503} {
		addr := servidorEco(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(st)
			_, _ = w.Write([]byte(`{"error":{"code":"x","message":"y"}}`))
		})
		c := &http1{addr: addr, timeout: 5 * time.Second}
		resp, err := c.fazer("POST", "/x", []byte("{}"), nil)
		if err != nil {
			t.Fatalf("status %d virou erro de transporte: %v", st, err)
		}
		if resp.status != st {
			t.Errorf("status = %d, esperado %d", resp.status, st)
		}
		if !strings.Contains(string(resp.corpo), "error") {
			t.Errorf("status %d veio sem corpo", st)
		}
	}
}

// Resposta sem corpo (204) não pode travar esperando bytes que nunca vêm.
func TestRespostaSemCorpoNaoTrava(t *testing.T) {
	addr := servidorEco(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	c := &http1{addr: addr, timeout: 3 * time.Second}

	pronto := make(chan struct{})
	go func() {
		defer close(pronto)
		resp, err := c.fazer("POST", "/x", nil, nil)
		if err != nil {
			t.Errorf("204 deu erro: %v", err)
			return
		}
		if resp.status != 204 || len(resp.corpo) != 0 {
			t.Errorf("resp = %d %q", resp.status, resp.corpo)
		}
	}()
	select {
	case <-pronto:
	case <-time.After(10 * time.Second):
		t.Fatal("travou esperando corpo de um 204")
	}
}

// Resposta em chunked encoding: o servidor Go usa isso quando não sabe o
// tamanho de antemão, que é o caso de qualquer handler que não fixe
// Content-Length.
func TestEntendeRespostaEmChunks(t *testing.T) {
	addr := servidorEco(t, func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Skip("sem Flusher")
		}
		_, _ = w.Write([]byte(`{"accepted":`))
		fl.Flush()
		_, _ = w.Write([]byte(`7,"duplicates":0}`))
	})

	c := &http1{addr: addr, timeout: 5 * time.Second}
	resp, err := c.fazer("POST", "/x", []byte("{}"), nil)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := campoInt(resp.corpo, "accepted")
	if !ok || n != 7 {
		t.Errorf("corpo em chunks veio como %q", resp.corpo)
	}
}

// Servidor inalcançável dá erro rápido, sem travar o device com o rádio ligado.
func TestServidorInalcancavelNaoTrava(t *testing.T) {
	c := &http1{addr: "127.0.0.1:1", timeout: 2 * time.Second}
	pronto := make(chan struct{})
	go func() {
		defer close(pronto)
		if _, err := c.fazer("POST", "/x", []byte("{}"), nil); err == nil {
			t.Error("conexão recusada devolveu sucesso")
		}
	}()
	select {
	case <-pronto:
	case <-time.After(10 * time.Second):
		t.Fatal("travou tentando conectar")
	}
}

// Servidor que aceita e não responde tem que estourar o timeout, não pendurar
// o device pra sempre.
func TestServidorMudoEstouraOTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(30 * time.Second) // nunca responde
	}()

	c := &http1{addr: l.Addr().String(), timeout: time.Second}
	inicio := time.Now()
	if _, err := c.fazer("POST", "/x", []byte("{}"), nil); err == nil {
		t.Fatal("servidor mudo devolveu sucesso")
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Errorf("levou %v pra desistir, o timeout era de 1s", d)
	}
}

// Cabeçalho gigante não pode virar alocação sem teto num device com 512 KB.
func TestCabecalhoGiganteNaoEstouraAMemoria(t *testing.T) {
	addr := servidorEco(t, func(w http.ResponseWriter, r *http.Request) {
		for i := range 200 {
			w.Header().Set(fmt.Sprintf("X-Lixo-%d", i), strings.Repeat("a", 200))
		}
		_, _ = w.Write([]byte("{}"))
	})
	c := &http1{addr: addr, timeout: 5 * time.Second}

	if _, err := c.fazer("POST", "/x", []byte("{}"), nil); err == nil {
		t.Error("resposta com cabeçalho absurdo foi aceita sem limite")
	}
}

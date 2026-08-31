// Package net é o sync do device com a API.
//
// Push apenas, de propósito: o device é autoridade local e nunca precisa
// puxar os próprios eventos de volta. Pull existe no contrato pro app e pra
// recuperação de um device zerado, e entra quando esse caso aparecer. Push
// sozinho já fecha o ciclo: evento nasce aqui, vira log no servidor.
package net

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ale/fera/internal/sig"
	"github.com/ale/fera/internal/sim"
)

// timeoutHTTP cobre a requisição inteira. Device sem prazo espera pra sempre
// com o rádio ligado, e o rádio é o maior consumidor de bateria que existe.
const timeoutHTTP = 20 * time.Second

// maxLote é o teto do api-contract. Cortar aqui é o que faz uma fila grande
// drenar em vários syncs em vez de tomar 400 e emperrar pra sempre.
const maxLote = 200

var (
	ErrNaoAutorizado = errors.New("net: credencial recusada")
	ErrPermanente    = errors.New("net: o servidor recusou o lote")
)

type Credenciais struct {
	BaseURL string
	PetID   string
	Token   string
	// DeviceID vai no header do caminho assinado: o servidor precisa achar a
	// chave ANTES de conferir o MAC, e o segredo não vai junto.
	DeviceID string
}

type Opcoes struct {
	// Timeout cobre conectar, escrever e ler. Zero usa o padrão.
	Timeout time.Duration
	// Espera é injetável pra que o teste não durma de verdade entre as
	// tentativas.
	Espera func(time.Duration)
	// Aleatorio dá o jitter do backoff. Sem jitter, todo device do mundo
	// volta junto depois de uma queda e derruba o servidor de novo.
	Aleatorio func() uint32
	// Agora é o relógio que entra na assinatura. Injetável pra teste; no
	// device vem do hal.
	Agora func() time.Time
}

type Client struct {
	creds Credenciais
	chave [32]byte
	conn  *http1
	opc   Opcoes

	// Buffer do corpo, reaproveitado entre syncs. O lote é limitado a 200
	// eventos, então ele estabiliza rápido e para de crescer.
	buf []byte
}

func New(creds Credenciais, opc Opcoes) *Client {
	if opc.Timeout == 0 {
		opc.Timeout = timeoutHTTP
	}
	if opc.Espera == nil {
		opc.Espera = time.Sleep
	}
	if opc.Aleatorio == nil {
		var n uint32
		opc.Aleatorio = func() uint32 { n = n*1664525 + 1013904223; return n }
	}
	return &Client{
		creds: creds,
		chave: sig.Chave(creds.Token),
		conn:  &http1{addr: enderecoDe(creds.BaseURL), timeout: opc.Timeout},
		opc:   opc,
	}
}

// Sync empurra os pendentes e devolve quantos o servidor passou a ter.
//
// DUPLICADO CONTA COMO ENVIADO. O servidor já tem aquele evento, então ele
// pode sair da fila local. Contar só o accepted deixaria preso pra sempre
// todo evento cujo ACK se perdeu no caminho, e a fila encheria sozinha.
func (c *Client) Sync(pend []sim.Event) (int, error) {
	if len(pend) == 0 {
		return 0, nil // não liga o rádio à toa
	}
	if len(pend) > maxLote {
		pend = pend[:maxLote]
	}

	c.buf = codificaLote(c.buf[:0], pend)
	caminho := "/v1/pets/" + c.creds.PetID + "/events"

	var ultimo error
	for tentativa := range 3 {
		if tentativa > 0 {
			c.opc.Espera(c.backoff(tentativa))
		}
		n, err := c.envia(caminho, c.buf)
		if err == nil {
			return n, nil
		}
		ultimo = err
		if errors.Is(err, ErrPermanente) || errors.Is(err, ErrNaoAutorizado) {
			return 0, err // insistir em 4xx só gasta bateria e rádio
		}
	}
	return 0, ultimo
}

func (c *Client) envia(caminho string, corpo []byte) (int, error) {
	resp, err := c.conn.fazer("POST", caminho, corpo, c.autentica("POST", caminho, corpo))
	if err != nil {
		return 0, fmt.Errorf("net: %w", err)
	}

	switch {
	case resp.status == 401:
		return 0, ErrNaoAutorizado
	case resp.status >= 400 && resp.status < 500:
		return 0, fmt.Errorf("%w: status %d", ErrPermanente, resp.status)
	case resp.status != 200:
		return 0, fmt.Errorf("net: status %d", resp.status)
	}

	aceitos, ok1 := campoInt(resp.corpo, "accepted")
	dupes, ok2 := campoInt(resp.corpo, "duplicates")
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("net: resposta sem accepted/duplicates: %s", resp.corpo)
	}
	return int(aceitos + dupes), nil
}

// enderecoDe extrai host:porta da URL base.
//
// À mão em vez de net/url: o parser da stdlib traz mais do que este device
// precisa, e a URL aqui é sempre http://host[:porta], vinda do register.
func enderecoDe(base string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if !strings.Contains(s, ":") {
		s += ":80"
	}
	return s
}

// backoff exponencial com jitter. Sem jitter, todo device volta junto depois
// de uma queda e derruba o servidor de novo no mesmo instante.
func (c *Client) backoff(tentativa int) time.Duration {
	base := time.Second << (tentativa - 1)
	jitter := time.Duration(c.opc.Aleatorio()%uint32(base/time.Millisecond)) * time.Millisecond
	return base + jitter/2
}

// Registrar cria device e pet no servidor e devolve as credenciais.
//
// É a única chamada sem autenticação, porque é por onde a credencial nasce. O
// pet_id vem do SERVIDOR, nunca é escolhido aqui: se o device pudesse pedir um
// pet_id, qualquer um se registraria no bicho de outro.
//
// O token aparece uma vez só. Quem chama grava na flash imediatamente ou
// perde o acesso.
func Registrar(baseURL string, opc Opcoes) (Credenciais, error) {
	c := New(Credenciais{BaseURL: baseURL}, opc)

	resp, err := c.conn.fazer("POST", "/v1/devices/register", nil, nil)
	if err != nil {
		return Credenciais{}, fmt.Errorf("net: registrar: %w", err)
	}
	if resp.status != 201 {
		return Credenciais{}, fmt.Errorf("net: registrar: status %d", resp.status)
	}

	pet, ok1 := campoTexto(resp.corpo, "pet_id")
	tok, ok2 := campoTexto(resp.corpo, "token")
	dev, ok3 := campoTexto(resp.corpo, "device_id")
	if !ok1 || !ok2 || !ok3 {
		return Credenciais{}, fmt.Errorf("net: registro incompleto: %s", resp.corpo)
	}
	return Credenciais{BaseURL: baseURL, PetID: pet, Token: tok, DeviceID: dev}, nil
}

// autentica assina a requisição em vez de mandar o token.
//
// O segredo NUNCA cruza o fio. Isso é o que torna HTTP puro aceitável neste
// device: TLS custaria +168 KB de RAM num chip cujo stack de WiFi já come 345
// dos 512 KB (ver docs/06). Perde-se sigilo do conteúdo, não autenticidade.
//
// Sem DeviceID cai no Bearer, que é o caminho do app sobre HTTPS e o que os
// testes mais antigos exercitam.
func (c *Client) autentica(metodo, caminho string, corpo []byte) []cabecalho {
	if c.creds.DeviceID == "" {
		return []cabecalho{{"Authorization", "Bearer " + c.creds.Token}}
	}
	agora := c.agora()
	return []cabecalho{
		{sig.HeaderDevice, c.creds.DeviceID},
		{sig.HeaderTimestamp, strconv.FormatInt(agora.Unix(), 10)},
		{sig.HeaderAssinatura, sig.Assinar(c.chave, metodo, caminho, agora, corpo)},
	}
}

func (c *Client) agora() time.Time {
	if c.opc.Agora != nil {
		return c.opc.Agora()
	}
	return time.Now()
}

package net

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Cliente HTTP/1.1 escrito à mão sobre TCP.
//
// POR QUE NÃO net/http: ele linka crypto/tls, crypto/x509 e math/big
// incondicionalmente, mesmo que a URL seja http://. Medido no xiao-esp32s3:
// 415 KB de flash e 189 KB de RAM com net/http, contra 89 KB e 40 KB com
// net + bufio. Num chip cujo stack de WiFi já usa 345 KB dos 512 KB de SRAM,
// essa diferença é entre caber e não caber.
//
// POR QUE NÃO uma lib: as que existem (soypat/lneto e antecessoras) trazem a
// pilha TCP/IP inteira em userspace, e o espradio já entrega sockets. Trocar
// uma pilha funcionando por outra pra ganhar a moldura de HTTP não paga.
//
// ESTE código roda nos DOIS alvos, host e device, de propósito. Um transporte
// no teste e outro na placa seria o pior tipo de build tag: o caminho que roda
// no hardware nunca seria exercitado.

const (
	// maxCabecalho limita a resposta que o device topa ler antes do corpo.
	// Sem teto, um servidor hostil (ou um proxy confuso) faz o device alocar
	// até morrer.
	maxCabecalho = 4 << 10
	maxCorpo     = 64 << 10
)

var (
	errRespostaTorta   = errors.New("net: resposta HTTP malformada")
	errCabecalhoGrande = errors.New("net: cabeçalho de resposta grande demais")
)

type cabecalho struct{ nome, valor string }

type resposta struct {
	status int
	corpo  []byte
}

type http1 struct {
	addr    string // host:porta
	timeout time.Duration
}

func (c *http1) fazer(metodo, caminho string, corpo []byte, extras []cabecalho) (resposta, error) {
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return resposta{}, err
	}
	defer conn.Close()

	// Um prazo pra requisição inteira. Device sem prazo espera pra sempre com
	// o rádio ligado, e o rádio é o maior consumidor de bateria que existe.
	if c.timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.timeout))
	}

	if err := escreveRequisicao(conn, c.addr, metodo, caminho, corpo, extras); err != nil {
		return resposta{}, err
	}
	return leResposta(conn)
}

func escreveRequisicao(w io.Writer, host, metodo, caminho string, corpo []byte, extras []cabecalho) error {
	var b []byte
	b = append(b, metodo...)
	b = append(b, ' ')
	b = append(b, caminho...)
	b = append(b, " HTTP/1.1\r\nHost: "...)
	b = append(b, host...)
	b = append(b, "\r\nContent-Type: application/json\r\nContent-Length: "...)
	b = strconv.AppendInt(b, int64(len(corpo)), 10)

	// Connection: close simplifica tudo. O device sincroniza uma ou duas vezes
	// por dia; manter conexão viva entre janelas de sync não economiza nada e
	// custa estado pra gerenciar.
	b = append(b, "\r\nConnection: close\r\n"...)

	for _, h := range extras {
		b = append(b, h.nome...)
		b = append(b, ": "...)
		b = append(b, h.valor...)
		b = append(b, "\r\n"...)
	}
	b = append(b, "\r\n"...)
	b = append(b, corpo...)

	_, err := w.Write(b)
	return err
}

func leResposta(r io.Reader) (resposta, error) {
	br := bufio.NewReaderSize(io.LimitReader(r, maxCabecalho+maxCorpo), 512)

	linha, err := br.ReadString('\n')
	if err != nil {
		return resposta{}, err
	}
	status, err := statusDaLinha(linha)
	if err != nil {
		return resposta{}, err
	}

	tam := -1
	chunked := false
	lidos := len(linha)
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return resposta{}, err
		}
		lidos += len(h)
		if lidos > maxCabecalho {
			return resposta{}, errCabecalhoGrande
		}
		if h == "\r\n" || h == "\n" {
			break
		}
		nome, valor, ok := strings.Cut(h, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(nome)) {
		case "content-length":
			tam, _ = strconv.Atoi(strings.TrimSpace(valor))
		case "transfer-encoding":
			chunked = strings.Contains(strings.ToLower(valor), "chunked")
		}
	}

	corpo, err := leCorpo(br, tam, chunked)
	if err != nil {
		return resposta{}, err
	}
	return resposta{status: status, corpo: corpo}, nil
}

func statusDaLinha(linha string) (int, error) {
	// "HTTP/1.1 200 OK"
	_, resto, ok := strings.Cut(strings.TrimSpace(linha), " ")
	if !ok {
		return 0, errRespostaTorta
	}
	cod, _, _ := strings.Cut(resto, " ")
	n, err := strconv.Atoi(cod)
	if err != nil {
		return 0, errRespostaTorta
	}
	return n, nil
}

// leCorpo trata os três casos que um servidor Go produz: tamanho conhecido,
// chunked (quando o handler não fixa Content-Length) e nenhum corpo.
func leCorpo(br *bufio.Reader, tam int, chunked bool) ([]byte, error) {
	switch {
	case chunked:
		return leChunks(br)
	case tam == 0:
		return nil, nil
	case tam > 0:
		if tam > maxCorpo {
			return nil, errCabecalhoGrande
		}
		b := make([]byte, tam)
		if _, err := io.ReadFull(br, b); err != nil {
			return nil, err
		}
		return b, nil
	default:
		// Sem Content-Length e sem chunked: o corpo vai até o fim da conexão,
		// que é o que Connection: close garante.
		return io.ReadAll(io.LimitReader(br, maxCorpo))
	}
}

func leChunks(br *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		linha, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		// o tamanho vem em hex, e pode ter extensão depois de ';'
		hexa, _, _ := strings.Cut(strings.TrimSpace(linha), ";")
		n, err := strconv.ParseInt(hexa, 16, 32)
		if err != nil {
			return nil, errRespostaTorta
		}
		if n == 0 {
			return out, nil
		}
		if len(out)+int(n) > maxCorpo {
			return nil, errCabecalhoGrande
		}
		pedaco := make([]byte, n)
		if _, err := io.ReadFull(br, pedaco); err != nil {
			return nil, err
		}
		out = append(out, pedaco...)
		if _, err := br.Discard(2); err != nil { // o \r\n depois do chunk
			return nil, err
		}
	}
}

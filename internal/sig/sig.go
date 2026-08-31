// Package sig assina requisição do device pro servidor.
//
// Existe por uma razão medida, não estética: o crypto/tls do Go custa +372 KB
// de flash e +168 KB de RAM no ESP32-S3, e o stack de WiFi (espradio) sozinho
// já usa 345 KB dos 512 KB de SRAM. Não cabe. Assinar com BLAKE2s com chave
// custa +2,5 KB de flash e +672 bytes de RAM. Ver docs/06.
//
// BLAKE2s e não HMAC-SHA256: qualquer coisa do crypto/sha256 arrasta o módulo
// crypto/internal/fips140 inteiro, e com ele AES, SHA-3 e SHA-512 que nunca
// são chamados. BLAKE2s está fora do módulo, é feito pra 32 bits, e já vem
// com modo com chave, dispensando a construção HMAC.
//
// A string canônica mora AQUI, num pacote que device e servidor importam.
// Duas cópias divergiriam no primeiro campo novo, e o sintoma seria 401 sem
// explicação em campo.
//
// O QUE ISTO DÁ: autenticidade e integridade. O segredo nunca cruza o fio.
// O QUE NÃO DÁ: sigilo. Quem estiver no caminho lê o treino. Quando isso
// pesar, o caminho é BearSSL via CGo (~7 KB de RAM), não o crypto/tls do Go.
package sig

import (
	"crypto/subtle"
	"strconv"
	"time"

	"golang.org/x/crypto/blake2s"
)

// Nomes dos headers. Estáveis: mudar qualquer um derruba todo device em campo.
const (
	HeaderDevice     = "X-Fera-Device"
	HeaderTimestamp  = "X-Fera-Timestamp"
	HeaderAssinatura = "X-Fera-Signature"
	// HeaderHora leva o relógio do servidor na recusa, pra que um device sem
	// RTC saiba que está fora da janela e possa se corrigir.
	HeaderHora = "X-Fera-Time"
)

// JanelaPadrao é generosa de propósito. O device não tem RTC confiável depois
// de um reboot a frio (docs/06), e uma janela apertada o deixaria sem
// conseguir autenticar até acertar o relógio, que ele só acerta sincronizando.
//
// Isso custa pouco porque a defesa de verdade contra replay não é o relógio:
// reenviar um lote capturado insere os mesmos ULIDs, o ON CONFLICT DO NOTHING
// os descarta, e o replay vira no-op. A janela é cinto e suspensório.
const JanelaPadrao = 24 * time.Hour

// Chave deriva a chave de assinatura do token.
//
// Derivada, e não o token direto: assim o servidor guarda a chave sem guardar
// o token, e um dump de backup não entrega a credencial que o device usa pra
// tudo. Não resolve o problema maior de MAC simétrico (quem lê o banco
// consegue ASSINAR), e a saída pra isso é Ed25519 com a coluna pubkey que a
// tabela devices já tem. Custa ~38 KB de RAM no device por causa do fips140,
// então fica pra quando pesar mais que os 672 bytes de hoje.
func Chave(token string) [32]byte {
	return blake2s.Sum256(append([]byte("fera-sign-v1\x00"), token...))
}

// Assinar devolve o MAC em hex.
//
// Assina método, caminho, instante e um digest do corpo. Os quatro entram
// porque trocar cada um é um ataque diferente: método muda a operação,
// caminho muda o pet, instante permite reuso e corpo muda os eventos.
func Assinar(chave [32]byte, metodo, caminho string, quando time.Time, corpo []byte) string {
	h, err := blake2s.New256(chave[:])
	if err != nil {
		return "" // só acontece com chave de tamanho errado, e ela é [32]byte
	}
	digest := blake2s.Sum256(corpo)

	// Separador que não pode aparecer nos campos, pra que
	// ("A","BC") e ("AB","C") não assinem igual.
	h.Write([]byte(metodo))
	h.Write([]byte{0})
	h.Write([]byte(caminho))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(quando.Unix(), 10)))
	h.Write([]byte{0})
	h.Write(digest[:])

	var soma [32]byte
	h.Sum(soma[:0])
	return hexa(soma[:])
}

// Confere compara em TEMPO CONSTANTE. Comparação com saída antecipada vaza,
// por tempo, quantos caracteres do prefixo estão certos, e isso deixa
// descobrir a assinatura byte a byte em vez de por força bruta.
func Confere(chave [32]byte, metodo, caminho string, quando time.Time, corpo []byte, assinatura string) bool {
	esperada := Assinar(chave, metodo, caminho, quando, corpo)
	if esperada == "" || len(assinatura) != len(esperada) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(esperada), []byte(assinatura)) == 1
}

// DentroDaJanela diz se o instante da requisição é plausível.
func DentroDaJanela(quando, agora time.Time, janela time.Duration) bool {
	d := agora.Sub(quando)
	if d < 0 {
		d = -d
	}
	return d <= janela
}

const digitos = "0123456789abcdef"

// hexa escrito à mão: encoding/hex é barato, mas este pacote roda no device e
// cada import a menos é binário a menos.
func hexa(b []byte) string {
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digitos[v>>4]
		out[i*2+1] = digitos[v&0x0F]
	}
	return string(out)
}

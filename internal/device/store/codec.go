package store

import (
	"encoding/binary"
	"time"

	"github.com/ale/fera/internal/sim"
)

// unixUTC devolve o instante em UTC. sim.Event.At é time.Time na borda, mas
// o sim só lê .Unix(), então o segundo é a única coisa que precisa sobreviver.
func unixUTC(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// Serialização binária de tamanho fixo, escrita à mão.
//
// Sem encoding/json no device: além de puxar reflection e inchar o binário,
// JSON tem tamanho variável, e registro de tamanho variável não dá pra
// enfileirar em ring buffer de NOR sem um índice separado que também precisa
// ser escrito. Tamanho fixo faz o índice ser a aritmética.
//
// Só binary.BigEndian daqui: as funções PutUint são aritmética pura. É
// binary.Write que usa reflection, e essa não entra.

const (
	// status só transiciona zerando bit, que é o que a NOR permite sem apagar:
	// 0xFF -> 0xFE -> 0xFC.
	statusLivre = 0xFF
	statusGrav  = 0xFE
	statusSync  = 0xFC

	tamID      = 26 // ULID em base32
	tamPetID   = 36 // UUID com hífens
	tamRegFila = 80

	// deslocamentos dentro do registro de fila
	offStatus  = 0
	offSeq     = 1
	offID      = 5
	offAt      = 31
	offKind    = 39
	offKcal    = 40
	offZone    = 42
	offMinutes = 43
	offPeer    = 45
	offCRCFila = 71

	tamRegEstado = 112

	// Credenciais: URL de até 128, pet_id de 36 e token de 64.
	tamURL      = 128
	tamToken    = 64
	tamRegCreds = 1 + tamURL + tamPetID + tamPetID + tamToken + 2
	offCredsURL = 1
	offCredsPet = offCredsURL + tamURL
	offCredsDev = offCredsPet + tamPetID
	offCredsTok = offCredsDev + tamPetID
	offCredsCRC = offCredsTok + tamToken
	offEstSeq   = 1
	offEstCorpo = 5
	offEstCRC   = 110
)

func poeTexto(dst []byte, s string) bool {
	if len(s) > len(dst) {
		return false
	}
	for i := range dst {
		dst[i] = 0
	}
	copy(dst, s)
	return true
}

func pegaTexto(src []byte) string {
	n := 0
	for n < len(src) && src[n] != 0 {
		n++
	}
	return string(src[:n])
}

func codificaEvento(dst []byte, ev sim.Event, seq uint32) bool {
	for i := range dst {
		dst[i] = 0xFF
	}
	if !poeTexto(dst[offID:offID+tamID], ev.ID) {
		return false
	}
	if !poeTexto(dst[offPeer:offPeer+tamID], ev.PeerID) {
		return false
	}
	dst[offStatus] = statusGrav
	binary.BigEndian.PutUint32(dst[offSeq:], seq)
	binary.BigEndian.PutUint64(dst[offAt:], uint64(ev.At.Unix()))
	dst[offKind] = byte(ev.Kind)
	binary.BigEndian.PutUint16(dst[offKcal:], ev.Kcal)
	dst[offZone] = ev.Zone
	binary.BigEndian.PutUint16(dst[offMinutes:], ev.Minutes)
	binary.BigEndian.PutUint16(dst[offCRCFila:], crc16(dst[offSeq:offCRCFila]))
	return true
}

func decodificaEvento(src []byte) (sim.Event, uint32, bool) {
	if binary.BigEndian.Uint16(src[offCRCFila:]) != crc16(src[offSeq:offCRCFila]) {
		return sim.Event{}, 0, false
	}
	return sim.Event{
		ID:      pegaTexto(src[offID : offID+tamID]),
		At:      unixUTC(int64(binary.BigEndian.Uint64(src[offAt:]))),
		Kind:    sim.Kind(src[offKind]),
		Kcal:    binary.BigEndian.Uint16(src[offKcal:]),
		Zone:    src[offZone],
		Minutes: binary.BigEndian.Uint16(src[offMinutes:]),
		PeerID:  pegaTexto(src[offPeer : offPeer+tamID]),
	}, binary.BigEndian.Uint32(src[offSeq:]), true
}

func codificaEstado(dst []byte, s sim.State, seq uint32) bool {
	for i := range dst {
		dst[i] = 0xFF
	}
	c := dst[offEstCorpo:]
	if !poeTexto(c[1:1+tamPetID], s.PetID) {
		return false
	}
	if !poeTexto(c[67:67+tamID], s.LastID) {
		return false
	}
	c[0] = s.SchemaVer
	binary.BigEndian.PutUint32(c[37:], uint32(s.Stats.Vigor))
	binary.BigEndian.PutUint32(c[41:], uint32(s.Stats.Animo))
	binary.BigEndian.PutUint32(c[45:], uint32(s.Stats.Saude))
	binary.BigEndian.PutUint32(c[49:], uint32(s.Stats.Vinculo))
	c[53] = byte(s.Stage)
	c[54] = byte(s.Trait)
	binary.BigEndian.PutUint32(c[55:], s.Growth)
	binary.BigEndian.PutUint64(c[59:], uint64(s.LastAtUnix))
	binary.BigEndian.PutUint64(c[93:], uint64(s.LastEncAtUnix))
	binary.BigEndian.PutUint32(c[101:], uint32(s.CarrySec))

	dst[0] = statusGrav
	binary.BigEndian.PutUint32(dst[offEstSeq:], seq)
	binary.BigEndian.PutUint16(dst[offEstCRC:], crc16(dst[offEstSeq:offEstCRC]))
	return true
}

func decodificaEstado(src []byte) (sim.State, uint32, bool) {
	if src[0] != statusGrav {
		return sim.State{}, 0, false
	}
	if binary.BigEndian.Uint16(src[offEstCRC:]) != crc16(src[offEstSeq:offEstCRC]) {
		return sim.State{}, 0, false
	}
	c := src[offEstCorpo:]
	return sim.State{
		SchemaVer: c[0],
		PetID:     pegaTexto(c[1 : 1+tamPetID]),
		Stats: sim.Stats{
			Vigor:   int32(binary.BigEndian.Uint32(c[37:])),
			Animo:   int32(binary.BigEndian.Uint32(c[41:])),
			Saude:   int32(binary.BigEndian.Uint32(c[45:])),
			Vinculo: int32(binary.BigEndian.Uint32(c[49:])),
		},
		Stage:         sim.Stage(c[53]),
		Trait:         sim.Trait(c[54]),
		Growth:        binary.BigEndian.Uint32(c[55:]),
		LastAtUnix:    int64(binary.BigEndian.Uint64(c[59:])),
		LastID:        pegaTexto(c[67 : 67+tamID]),
		LastEncAtUnix: int64(binary.BigEndian.Uint64(c[93:])),
		CarrySec:      int32(binary.BigEndian.Uint32(c[101:])),
	}, binary.BigEndian.Uint32(src[offEstSeq:]), true
}

// crc16 CCITT. Existe pra distinguir registro completo de registro cortado no
// meio por queda de energia: sem ele, meio registro parece registro válido.
func crc16(p []byte) uint16 {
	var c uint16 = 0xFFFF
	for _, b := range p {
		c ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

func codificaCreds(dst []byte, c credsBrutas) bool {
	for i := range dst {
		dst[i] = 0xFF
	}
	if !poeTexto(dst[offCredsURL:offCredsURL+tamURL], c.BaseURL) ||
		!poeTexto(dst[offCredsPet:offCredsPet+tamPetID], c.PetID) ||
		!poeTexto(dst[offCredsDev:offCredsDev+tamPetID], c.DeviceID) ||
		!poeTexto(dst[offCredsTok:offCredsTok+tamToken], c.Token) {
		return false
	}
	dst[0] = statusGrav
	binary.BigEndian.PutUint16(dst[offCredsCRC:], crc16(dst[offCredsURL:offCredsCRC]))
	return true
}

func decodificaCreds(src []byte) (credsBrutas, bool) {
	if src[0] != statusGrav {
		return credsBrutas{}, false
	}
	if binary.BigEndian.Uint16(src[offCredsCRC:]) != crc16(src[offCredsURL:offCredsCRC]) {
		return credsBrutas{}, false
	}
	return credsBrutas{
		BaseURL:  pegaTexto(src[offCredsURL : offCredsURL+tamURL]),
		PetID:    pegaTexto(src[offCredsPet : offCredsPet+tamPetID]),
		DeviceID: pegaTexto(src[offCredsDev : offCredsDev+tamPetID]),
		Token:    pegaTexto(src[offCredsTok : offCredsTok+tamToken]),
	}, true
}

// credsBrutas é o mesmo shape de Creds. Existe pra que o codec não precise
// conhecer o tipo público e o arquivo continue sendo só serialização.
type credsBrutas = Creds

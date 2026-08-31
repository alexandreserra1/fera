package store

import "github.com/ale/fera/internal/sim"

// Layout da flash:
//
//	setor 0 e 1   estado, gravado alternando entre os dois
//	setor 2       credenciais do device
//	setor 3..N    fila de pendentes, ring buffer append-only
//
// O estado usa DOIS setores porque gravar é apagar antes: se a energia cair
// entre o Erase e o Write, o setor fica vazio. Alternando, a queda destrói o
// registro novo e o anterior continua íntegro no outro setor. Um setor só
// seria uma janela em que o bicho some.
//
// A credencial tem setor PRÓPRIO porque é escrita uma vez na vida do device e
// lida em todo sync. Dividir setor com estado ou com a fila faria um
// apagamento de rotina levar o token junto, e sem token o bicho nunca mais
// sincroniza.
const (
	setoresEstado = 2
	setorCreds    = 2
	primeiroFila  = 3
)

type Store struct {
	f          Flash
	setorBytes int64
	setores    int64

	proxSeqEstado uint32
	setorEstado   int64 // onde vai o PRÓXIMO save

	slots       int64
	porSetor    int64
	proxSeqFila uint32
	cabeca      int64 // índice do próximo slot da fila

	// Buffers alocados uma vez, no Open. Nenhum make nos métodos: o laço do
	// device chama AppendPending a cada botão pressionado, e alocar ali acorda
	// o GC conservativo do TinyGo num device que deveria estar dormindo.
	bufReg    [tamRegFila]byte
	bufEstado [tamRegEstado]byte
	bufCreds  [tamRegCreds]byte
	ordem     []slotSeq
}

// slotSeq é par (slot, seq) usado pra ordenar a fila por ordem de gravação.
type slotSeq struct {
	slot int64
	seq  uint32
}

// Open lê o que já está na flash e reconstrói a posição da fila. Flash virgem
// é caso normal, não erro: é o primeiro boot do device.
func Open(f Flash, setorBytes int64, setores int) (*Store, error) {
	if int64(setores) <= primeiroFila {
		return nil, ErrPoucoSetor
	}
	s := &Store{
		f:          f,
		setorBytes: setorBytes,
		setores:    int64(setores),
		slots:      (int64(setores) - primeiroFila) * (setorBytes / tamRegFila),
		porSetor:   setorBytes / tamRegFila,
	}
	s.ordem = make([]slotSeq, 0, s.slots)
	if err := s.recuperaEstado(); err != nil {
		return nil, err
	}
	return s, s.recuperaFila()
}

func (s *Store) recuperaEstado() error {
	var melhorSeq uint32
	achou := false
	buf := s.bufEstado[:]

	for setor := int64(0); setor < setoresEstado; setor++ {
		if err := s.f.Read(setor*s.setorBytes, buf); err != nil {
			return err
		}
		_, seq, ok := decodificaEstado(buf)
		if ok && (!achou || seq > melhorSeq) {
			melhorSeq, achou = seq, true
			// o próximo save vai pro OUTRO setor
			s.setorEstado = (setor + 1) % setoresEstado
		}
	}
	if achou {
		s.proxSeqEstado = melhorSeq + 1
	}
	return nil
}

// recuperaFila acha a cabeça pelo maior seq gravado, não pelo primeiro slot
// livre. Depois que a fila dá a volta, existem slots livres ANTES dos
// gravados, e procurar o primeiro livre poria a cabeça no meio do histórico.
func (s *Store) recuperaFila() error {
	buf := s.bufReg[:]
	var maiorSeq uint32
	melhor := int64(-1)

	for i := int64(0); i < s.slots; i++ {
		if err := s.f.Read(s.offSlot(i), buf); err != nil {
			return err
		}
		if buf[offStatus] == statusLivre {
			continue
		}
		if _, seq, ok := decodificaEvento(buf); ok {
			if melhor < 0 || seq > maiorSeq {
				maiorSeq, melhor = seq, i
			}
		}
	}
	if melhor >= 0 {
		s.cabeca = (melhor + 1) % s.slots
		s.proxSeqFila = maiorSeq + 1
	}
	return nil
}

// offSlot pula o resto de cada setor de propósito.
//
// 4096 / 80 dá 51,2: tratar a fila como região contígua faria o registro 51
// começar no byte 4080 e ATRAVESSAR a fronteira do setor. Apagar um setor
// destruiria metade de um registro do setor seguinte, e o CRC do sobrevivente
// acusaria corrupção sem explicação. Registro nunca cruza fronteira de
// apagamento; os 16 bytes que sobram por setor são o preço.
func (s *Store) offSlot(i int64) int64 {
	return primeiroFila*s.setorBytes + (i/s.porSetor)*s.setorBytes + (i%s.porSetor)*tamRegFila
}

func (s *Store) setorDoSlot(i int64) int64 {
	return primeiroFila + i/s.porSetor
}

// SaveState grava o estado no setor da vez e alterna. Um Erase por save, e o
// desgaste fica dividido entre os dois setores.
//
// Chame só quando vale a pena: mudança de estágio, botão pressionado, antes de
// deep sleep, depois de sync. A cada tick de 5 min seriam 105 mil escritas por
// ano, e a NOR aguenta ~100 mil ciclos por setor.
func (s *Store) SaveState(st sim.State) error {
	buf := s.bufEstado[:]
	if !codificaEstado(buf, st, s.proxSeqEstado) {
		return ErrEstadoGrande
	}
	if err := s.f.Erase(s.setorEstado); err != nil {
		return err
	}
	if err := s.f.Write(s.setorEstado*s.setorBytes, buf); err != nil {
		return err
	}
	s.proxSeqEstado++
	s.setorEstado = (s.setorEstado + 1) % setoresEstado
	return nil
}

// LoadState devolve o registro íntegro mais recente. Um registro cortado no
// meio por queda de energia falha no CRC e perde pro outro setor.
func (s *Store) LoadState() (sim.State, error) {
	buf := s.bufEstado[:]
	var melhor sim.State
	var melhorSeq uint32
	achou := false

	for setor := int64(0); setor < setoresEstado; setor++ {
		if err := s.f.Read(setor*s.setorBytes, buf); err != nil {
			return sim.State{}, err
		}
		st, seq, ok := decodificaEstado(buf)
		if ok && (!achou || seq > melhorSeq) {
			melhor, melhorSeq, achou = st, seq, true
		}
	}
	if !achou {
		return sim.State{}, ErrVazio
	}
	return melhor, nil
}

// AppendPending enfileira um evento. Só escreve: nenhum Erase, nenhuma
// reescrita. É o que permite gravar a cada botão pressionado sem gastar o
// device, porque escrever num slot virgem não consome ciclo de apagamento.
func (s *Store) AppendPending(ev sim.Event) error {
	if err := s.liberaSlot(); err != nil {
		return err
	}
	buf := s.bufReg[:]
	if !codificaEvento(buf, ev, s.proxSeqFila) {
		return ErrEventoGrande
	}
	if err := s.f.Write(s.offSlot(s.cabeca), buf); err != nil {
		return err
	}
	s.proxSeqFila++
	s.cabeca = (s.cabeca + 1) % s.slots
	return nil
}

// liberaSlot garante que a cabeça aponta pra espaço virgem.
//
// Se o slot já foi usado, o setor inteiro precisa ser apagado, e isso só é
// permitido quando TODO registro dele já foi sincronizado. Se algum ainda
// estiver pendente, a fila está cheia de verdade e o chamador precisa
// sincronizar antes de gerar mais evento.
func (s *Store) liberaSlot() error {
	buf := s.bufReg[:]
	if err := s.f.Read(s.offSlot(s.cabeca), buf); err != nil {
		return err
	}
	if buf[offStatus] == statusLivre {
		return nil
	}

	setor := s.setorDoSlot(s.cabeca)
	primeiro := (setor - primeiroFila) * s.porSetor

	for i := primeiro; i < primeiro+s.porSetor && i < s.slots; i++ {
		if err := s.f.Read(s.offSlot(i), buf); err != nil {
			return err
		}
		if buf[offStatus] == statusGrav {
			return ErrFilaCheia
		}
	}
	return s.f.Erase(setor)
}

// Pending devolve os eventos ainda não sincronizados, em ordem de gravação.
// Registro com CRC quebrado é descartado em silêncio: um bit trocado na flash
// não pode derrubar o device nem contaminar o log com evento inventado.
func (s *Store) Pending() ([]sim.Event, error) {
	if err := s.varreFila(); err != nil {
		return nil, err
	}
	evs := make([]sim.Event, 0, len(s.ordem))
	buf := s.bufReg[:]
	for _, it := range s.ordem {
		if err := s.f.Read(s.offSlot(it.slot), buf); err != nil {
			return nil, err
		}
		if ev, _, ok := decodificaEvento(buf); ok {
			evs = append(evs, ev)
		}
	}
	return evs, nil
}

// varreFila recolhe os slots gravados em ordem de seq, reaproveitando o mesmo
// backing array. Insertion sort escrito à mão, não sort.Slice: sort.Slice usa
// reflect.Swapper, e reflect infla o binário no TinyGo. É a mesma razão pela
// qual o internal/sim não importa sort.
func (s *Store) varreFila() error {
	s.ordem = s.ordem[:0]
	buf := s.bufReg[:]

	for i := int64(0); i < s.slots; i++ {
		if err := s.f.Read(s.offSlot(i), buf); err != nil {
			return err
		}
		if buf[offStatus] != statusGrav {
			continue
		}
		_, seq, ok := decodificaEvento(buf)
		if !ok {
			continue // CRC quebrado: bit trocado não vira evento inventado
		}
		s.ordem = append(s.ordem, slotSeq{i, seq})
		for j := len(s.ordem) - 1; j > 0 && s.ordem[j].seq < s.ordem[j-1].seq; j-- {
			s.ordem[j], s.ordem[j-1] = s.ordem[j-1], s.ordem[j]
		}
	}
	return nil
}

// MarkSynced marca os n mais antigos como enviados. Só zera bits do byte de
// status (0xFE vira 0xFC), sem reescrever o registro e sem apagar setor: o
// espaço só volta quando um setor inteiro fica sincronizado.
func (s *Store) MarkSynced(n int) error {
	if n <= 0 {
		return nil
	}
	if err := s.varreFila(); err != nil {
		return err
	}
	if n > len(s.ordem) {
		n = len(s.ordem)
	}
	var marca [1]byte
	marca[0] = statusSync
	for _, it := range s.ordem[:n] {
		if err := s.f.Write(s.offSlot(it.slot)+offStatus, marca[:]); err != nil {
			return err
		}
	}
	return nil
}

// Creds é o que o device recebe do register e precisa guardar pra sempre.
type Creds struct {
	BaseURL string
	PetID   string
	// DeviceID vai no header do caminho assinado: o servidor acha a chave por
	// ele, e o token não precisa cruzar o fio.
	DeviceID string
	Token    string
}

// SaveCreds grava as credenciais. Uma vez na vida do device, normalmente.
func (s *Store) SaveCreds(c Creds) error {
	buf := s.bufCreds[:]
	if !codificaCreds(buf, c) {
		return ErrCredsGrande
	}
	if err := s.f.Erase(setorCreds); err != nil {
		return err
	}
	return s.f.Write(setorCreds*s.setorBytes, buf)
}

// LoadCreds devolve ErrVazio quando não há credencial utilizável, inclusive
// quando ela está corrompida: token lixo enviado ao servidor rende 401 sem
// explicação, e registrar de novo é a saída certa.
func (s *Store) LoadCreds() (Creds, error) {
	buf := s.bufCreds[:]
	if err := s.f.Read(setorCreds*s.setorBytes, buf); err != nil {
		return Creds{}, err
	}
	c, ok := decodificaCreds(buf)
	if !ok {
		return Creds{}, ErrVazio
	}
	return c, nil
}

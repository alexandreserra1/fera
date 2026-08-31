// Package store guarda estado e fila de eventos na flash do device.
//
// Tudo aqui roda no Mac: a única coisa que o hardware fornece é a interface
// Flash, e MemFlash a implementa fielmente em memória. É por isso que as
// regras de desgaste podem ser testadas sem gastar um device de verdade
// descobrindo que estavam erradas.
package store

import "errors"

var (
	ErrForaDaFlash = errors.New("store: acesso fora da flash")
	ErrFilaCheia   = errors.New("store: fila de pendentes cheia")
	// Não existe ErrCorrompido: registro com CRC quebrado é DESCARTADO em
	// silêncio, não reportado. Um bit trocado na flash não pode derrubar o
	// device nem virar evento inventado no log, e quem chama não tem o que
	// fazer com essa informação além de seguir sem o registro.
	ErrVazio = errors.New("store: nada gravado")

	// Sentinela em vez de fmt.Errorf com contexto: o skill firmware proíbe fmt
	// no device porque ele puxa reflection e infla o binário. Aqui o custo de
	// perder o valor no texto do erro é baixo, porque quem chama já sabe o que
	// passou.
	ErrPoucoSetor   = errors.New("store: setores de menos, mínimo 3")
	ErrEstadoGrande = errors.New("store: pet_id ou last_id longo demais pro registro")
	ErrEventoGrande = errors.New("store: id ou peer_id longo demais pro registro")
	ErrCredsGrande  = errors.New("store: url, pet_id ou token longo demais pro registro")
)

// Flash é o mínimo que o store precisa do hardware. Três métodos, e nenhum
// deles conhece evento, estado ou pet.
//
// A semântica é a da NOR real, não a de um arquivo: Write só consegue zerar
// bit. Voltar bit pra 1 exige Erase, e Erase só funciona em setor inteiro.
// É essa assimetria que dita o formato dos registros lá embaixo.
type Flash interface {
	Read(off int64, p []byte) error
	Write(off int64, p []byte) error
	Erase(setor int64) error
}

// MemFlash é NOR em memória, com a semântica de verdade.
//
// Não é um []byte com Read e Write: um fake que deixasse Write levantar bit
// faria todo o desenho de ring buffer append-only passar no teste e falhar na
// placa, que é o pior tipo de fake que existe.
type MemFlash struct {
	bytes      []byte
	setorBytes int64

	// Apagamentos conta Erase por setor. É o contador de desgaste: NVS
	// aguenta ~100 mil ciclos por setor, e o docs/06 diz que salvar a cada
	// tick mata o device em um ano. Sem medir, isso não se descobre.
	Apagamentos []int
	Escritas    int
}

func NewMemFlash(setores int, setorBytes int64) *MemFlash {
	f := &MemFlash{
		bytes:       make([]byte, int64(setores)*setorBytes),
		setorBytes:  setorBytes,
		Apagamentos: make([]int, setores),
	}
	for i := range f.bytes {
		f.bytes[i] = 0xFF // flash virgem é tudo 1
	}
	return f
}

func (f *MemFlash) SetorBytes() int64 { return f.setorBytes }
func (f *MemFlash) Setores() int      { return len(f.Apagamentos) }

func (f *MemFlash) Read(off int64, p []byte) error {
	if off < 0 || off+int64(len(p)) > int64(len(f.bytes)) {
		return ErrForaDaFlash
	}
	copy(p, f.bytes[off:])
	return nil
}

// Write faz AND com o que já está lá. É assim que a NOR funciona: gravar
// pode apagar bit, nunca acender. Escrever 0xFF em cima de qualquer coisa é
// no-op, e escrever 0x00 em cima de 0x00 também.
func (f *MemFlash) Write(off int64, p []byte) error {
	if off < 0 || off+int64(len(p)) > int64(len(f.bytes)) {
		return ErrForaDaFlash
	}
	for i, b := range p {
		f.bytes[off+int64(i)] &= b
	}
	f.Escritas++
	return nil
}

func (f *MemFlash) Erase(setor int64) error {
	if setor < 0 || setor >= int64(len(f.Apagamentos)) {
		return ErrForaDaFlash
	}
	ini := setor * f.setorBytes
	for i := ini; i < ini+f.setorBytes; i++ {
		f.bytes[i] = 0xFF
	}
	f.Apagamentos[setor]++
	return nil
}

// Corromper zera bits pra simular queda de energia no meio da escrita e bit
// rot. Devolve false quando a máscara não mudou nada, porque corrupção que não
// corrompe é como um teste de robustez passa sem testar robustez nenhuma.
func (f *MemFlash) Corromper(off int64, mascara byte) bool {
	antes := f.bytes[off]
	f.bytes[off] &= mascara
	return f.bytes[off] != antes
}

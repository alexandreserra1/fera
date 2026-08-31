package main

import (
	"os"

	"github.com/ale/fera/internal/device/store"
)

// FileFlash é a flash do device num arquivo. Mesma semântica NOR do MemFlash
// (Write só zera bit, Erase devolve o setor pra 0xFF), só que persistente:
// é o que faz o bicho sobreviver a fechar e reabrir o programa.
//
// Mora aqui e não em internal/device/store de propósito: importa "os", e o
// pacote store é compilado pro ESP32.
type FileFlash struct {
	f          *os.File
	setorBytes int64
	setores    int64
}

func AbrirFileFlash(caminho string, setores int, setorBytes int64) (*FileFlash, error) {
	f, err := os.OpenFile(caminho, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	tam := int64(setores) * setorBytes
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < tam {
		// flash virgem é tudo 1
		virgem := make([]byte, tam-info.Size())
		for i := range virgem {
			virgem[i] = 0xFF
		}
		if _, err := f.WriteAt(virgem, info.Size()); err != nil {
			return nil, err
		}
	}
	return &FileFlash{f: f, setorBytes: setorBytes, setores: int64(setores)}, nil
}

func (f *FileFlash) Close() error { return f.f.Close() }

func (f *FileFlash) Read(off int64, p []byte) error {
	if off < 0 || off+int64(len(p)) > f.setores*f.setorBytes {
		return store.ErrForaDaFlash
	}
	_, err := f.f.ReadAt(p, off)
	return err
}

// Write faz AND com o que já está lá, igual à NOR de verdade. Um FileFlash que
// sobrescrevesse deixaria o ring buffer append-only passar aqui e falhar na
// placa, que é o pior tipo de simulação.
func (f *FileFlash) Write(off int64, p []byte) error {
	if off < 0 || off+int64(len(p)) > f.setores*f.setorBytes {
		return store.ErrForaDaFlash
	}
	atual := make([]byte, len(p))
	if _, err := f.f.ReadAt(atual, off); err != nil {
		return err
	}
	for i := range atual {
		atual[i] &= p[i]
	}
	_, err := f.f.WriteAt(atual, off)
	return err
}

func (f *FileFlash) Erase(setor int64) error {
	if setor < 0 || setor >= f.setores {
		return store.ErrForaDaFlash
	}
	virgem := make([]byte, f.setorBytes)
	for i := range virgem {
		virgem[i] = 0xFF
	}
	_, err := f.f.WriteAt(virgem, setor*f.setorBytes)
	return err
}

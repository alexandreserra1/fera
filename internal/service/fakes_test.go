package service

import (
	"context"
	"sync"
	"time"

	"github.com/ale/fera/internal/sim"
)

// Fakes escritos à mão, ~40 linhas cada. Melhor que gomock: sem codegen, e o
// comportamento fica legível no mesmo arquivo que o teste que depende dele.

// fakeEvents guarda o log em memória. seq é o índice + 1, que é exatamente a
// semântica do BIGSERIAL: monotônico e na ordem de INSERÇÃO, não cronológica.
type fakeEvents struct {
	mu      sync.Mutex
	log     []sim.Event
	chamou  map[string]int // quantas vezes cada método foi chamado
	erroSet error
}

func novoFakeEvents(evs ...sim.Event) *fakeEvents {
	return &fakeEvents{log: append([]sim.Event{}, evs...), chamou: map[string]int{}}
}

func (f *fakeEvents) conta(m string) {
	f.chamou[m]++
}

func (f *fakeEvents) vezes(m string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chamou[m]
}

func (f *fakeEvents) Append(_ context.Context, _ string, evs []sim.Event) ([]string, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conta("Append")
	if f.erroSet != nil {
		return nil, 0, f.erroSet
	}
	visto := map[string]bool{}
	for _, e := range f.log {
		visto[e.ID] = true
	}
	var novos []string
	for _, e := range evs {
		if visto[e.ID] {
			continue
		}
		visto[e.ID] = true
		f.log = append(f.log, e)
		novos = append(novos, e.ID)
	}
	return novos, int64(len(f.log)), nil
}

func (f *fakeEvents) Since(_ context.Context, _ string, cursor int64, limit int) ([]sim.Event, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conta("Since")
	if f.erroSet != nil {
		return nil, 0, f.erroSet
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

func (f *fakeEvents) FirstAt(_ context.Context, _ string) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conta("FirstAt")
	if f.erroSet != nil {
		return time.Time{}, false, f.erroSet
	}
	if len(f.log) == 0 {
		return time.Time{}, false, nil
	}
	min := f.log[0].At
	for _, e := range f.log[1:] {
		if e.At.Before(min) {
			min = e.At
		}
	}
	return min, true, nil
}

type fakeSnapshots struct {
	mu     sync.Mutex
	estado sim.State
	seq    int64
	tem    bool
	chamou map[string]int
}

func novoFakeSnapshots() *fakeSnapshots {
	return &fakeSnapshots{chamou: map[string]int{}}
}

func (f *fakeSnapshots) vezes(m string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chamou[m]
}

func (f *fakeSnapshots) Load(_ context.Context, _ string) (sim.State, int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chamou["Load"]++
	return f.estado, f.seq, f.tem, nil
}

func (f *fakeSnapshots) Save(_ context.Context, _ string, s sim.State, seq int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chamou["Save"]++
	if f.tem && seq <= f.seq { // mesma regra do WHERE no SQL de verdade
		return nil
	}
	f.estado, f.seq, f.tem = s, seq, true
	return nil
}

func (f *fakeSnapshots) Delete(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chamou["Delete"]++
	f.estado, f.seq, f.tem = sim.State{}, 0, false
	return nil
}

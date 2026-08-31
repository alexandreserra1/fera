// Package service tem os casos de uso. Depende de interfaces declaradas aqui,
// nunca de pgx, nunca de net/http.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/maypok86/otter/v2"
	"golang.org/x/sync/singleflight"

	"github.com/ale/fera/internal/sim"
)

// pageSize é o tamanho da página do replay. Bate com o limite de lote do
// api-contract de propósito: os dois falam da mesma unidade de trabalho.
const pageSize = 200

// As interfaces moram no CONSUMIDOR e são mínimas. Nenhuma passa de 3 métodos.
// Se uma delas crescer pra 5, o service está fazendo coisa demais.

type eventStore interface {
	// Append devolve os IDs que entraram de fato, não só a contagem: a
	// reconciliação precisa saber QUAIS eventos são novos.
	Append(ctx context.Context, petID string, evs []sim.Event) ([]string, int64, error)
	Since(ctx context.Context, petID string, cursor int64, limit int) ([]sim.Event, int64, error)
	// FirstAt é o instante do evento cronologicamente mais antigo do pet, que
	// é a definição operacional de "quando esse bicho nasceu".
	FirstAt(ctx context.Context, petID string) (time.Time, bool, error)
}

type snapshotStore interface {
	Load(ctx context.Context, petID string) (sim.State, int64, bool, error)
	Save(ctx context.Context, petID string, s sim.State, seq int64) error
	Delete(ctx context.Context, petID string) error
}

// IngestResult é o que a borda transforma no corpo da resposta.
type IngestResult struct {
	Accepted   int
	Duplicates int
	Cursor     int64
}

type PetService struct {
	events    eventStore
	snapshots snapshotStore
	clock     func() time.Time
	tuning    sim.Tuning

	// Cache guarda State, nunca View. View depende de "agora", então cachear
	// View congelaria o bicho no instante do fold e o decaimento pararia de
	// andar durante o TTL inteiro. Guardando State, cada leitura projeta com
	// o relógio da hora e o decaimento sai de graça.
	cache *otter.Cache[string, sim.State]
	sf    singleflight.Group
}

func New(events eventStore, snapshots snapshotStore, clock func() time.Time, t sim.Tuning) *PetService {
	return &PetService{
		events:    events,
		snapshots: snapshots,
		clock:     clock,
		tuning:    t,
		// TTL curto de propósito: o estado muda a cada evento, e cache velho
		// de bicho é pior que cache miss.
		cache: otter.Must(&otter.Options[string, sim.State]{
			MaximumSize:      10_000,
			ExpiryCalculator: otter.ExpiryWriting[string, sim.State](2 * time.Minute),
		}),
	}
}

// Get devolve o bicho como o dono vê agora.
func (s *PetService) Get(ctx context.Context, petID string) (sim.View, error) {
	if st, ok := s.cache.GetIfPresent(petID); ok {
		return sim.Project(st, s.clock(), s.tuning), nil
	}

	// singleflight na chave do pet: cem devices pedindo o mesmo pet frio
	// viram um fold só, não cem.
	v, err, _ := s.sf.Do(petID, func() (any, error) {
		// Relê o cache DENTRO do voo. Sem isto, quem checou o cache logo antes
		// do primeiro voo popular sai do singleflight e abre um voo novo, que
		// vai ao banco de novo por um estado que já está em memória. A janela é
		// pequena, mas ela é exatamente o instante do thundering herd.
		if st, ok := s.cache.GetIfPresent(petID); ok {
			return st, nil
		}
		return s.foldDoStore(ctx, petID)
	})
	if err != nil {
		return sim.View{}, err
	}
	return sim.Project(v.(sim.State), s.clock(), s.tuning), nil
}

// Ingest grava o lote. Não folda: o estado é derivado na leitura, e foldar
// aqui faria o caminho de escrita pagar por um trabalho que talvez ninguém leia.
func (s *PetService) Ingest(ctx context.Context, petID string, evs []sim.Event) (IngestResult, error) {
	novos, cursor, err := s.events.Append(ctx, petID, evs)
	if err != nil {
		return IngestResult{}, fmt.Errorf("ingest do pet %s: %w", petID, err)
	}

	if err := s.reconcilia(ctx, petID, evs, novos); err != nil {
		return IngestResult{}, err
	}
	s.cache.Invalidate(petID)

	return IngestResult{
		Accepted:   len(novos),
		Duplicates: len(evs) - len(novos),
		Cursor:     cursor,
	}, nil
}

// reconcilia trata o evento atrasado. Fold só aplica evento posterior ao último
// já aplicado, então um evento mais velho que o snapshot seria descartado em
// silêncio no caminho incremental. A saída é jogar o snapshot fora e deixar o
// próximo Get refoldar do genesis.
//
// Olha SÓ os eventos novos. Duplicado nunca aplicaria sobre o snapshot (ele já
// está lá dentro), então considerá-lo aqui faria todo reenvio de lote — o
// caminho feliz de um retry — custar um replay do log inteiro.
//
// A pergunta "esse evento entraria?" é feita pro próprio sim (WouldApply), não
// reimplementada aqui: uma cópia da regra divergiria do Fold no primeiro
// refactor e o evento voltaria a sumir.
func (s *PetService) reconcilia(ctx context.Context, petID string, evs []sim.Event, novos []string) error {
	if len(novos) == 0 {
		return nil
	}
	st, _, ok, err := s.snapshots.Load(ctx, petID)
	if err != nil {
		return fmt.Errorf("reconciliar pet %s: %w", petID, err)
	}
	if !ok {
		return nil // sem snapshot, o próximo Get já refolda tudo
	}

	ehNovo := make(map[string]bool, len(novos))
	for _, id := range novos {
		ehNovo[id] = true
	}
	for _, ev := range evs {
		if !ehNovo[ev.ID] {
			continue
		}
		if !sim.WouldApply(st, ev) {
			if err := s.snapshots.Delete(ctx, petID); err != nil {
				return fmt.Errorf("invalidar snapshot do pet %s: %w", petID, err)
			}
			return nil
		}
	}
	return nil
}

func (s *PetService) foldDoStore(ctx context.Context, petID string) (sim.State, error) {
	st, seq, ok, err := s.snapshots.Load(ctx, petID)
	if err != nil {
		return sim.State{}, fmt.Errorf("get do pet %s: %w", petID, err)
	}

	if !ok {
		st, seq, err = s.genesis(ctx, petID)
		if err != nil {
			return sim.State{}, err
		}
	}

	inicio := seq
	for {
		evs, prox, err := s.events.Since(ctx, petID, seq, pageSize)
		if err != nil {
			return sim.State{}, fmt.Errorf("get do pet %s: %w", petID, err)
		}
		if len(evs) == 0 {
			break
		}
		st = sim.Fold(st, evs, s.tuning)
		seq = prox
		if len(evs) < pageSize {
			break
		}
	}

	if seq > inicio {
		if err := s.snapshots.Save(ctx, petID, st, seq); err != nil {
			return sim.State{}, fmt.Errorf("get do pet %s: %w", petID, err)
		}
	}
	s.cache.Set(petID, st)
	return st, nil
}

// genesis ancora o estado inicial no evento cronologicamente mais antigo, não
// no primeiro da página. As páginas vêm em ordem de seq (inserção), então o
// primeiro lido pode ser posterior a outro do mesmo lote: ancorar nele faria
// o Fold descartar tudo que veio antes.
//
// Pet sem nenhum evento nasce agora.
func (s *PetService) genesis(ctx context.Context, petID string) (sim.State, int64, error) {
	at, ok, err := s.events.FirstAt(ctx, petID)
	if err != nil {
		return sim.State{}, 0, fmt.Errorf("genesis do pet %s: %w", petID, err)
	}
	if !ok {
		at = s.clock()
	}
	return sim.Genesis(petID, at), 0, nil
}

// Events é o pull por cursor: o device pede o que apareceu desde o último seq
// que viu. Fino de propósito, é passagem direta pro store, mas existe porque a
// borda não deve conhecer o repo.
//
// Devolve o cursor pra continuar. Página vazia devolve o cursor de entrada, e
// é assim que o cliente sabe que alcançou o fim.
func (s *PetService) Events(ctx context.Context, petID string, cursor int64, limit int) ([]sim.Event, int64, error) {
	evs, prox, err := s.events.Since(ctx, petID, cursor, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("pull do pet %s: %w", petID, err)
	}
	return evs, prox, nil
}

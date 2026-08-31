// Package repo é a camada de pgx. SQL escrito à mão, sem ORM, sem codegen.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ale/fera/internal/sim"
)

// maxLimit é chão de defesa, não o contrato. O limite de 200 do api-contract
// é validado na borda; aqui só existe pra que um bug de camada de cima não
// vire SELECT sem teto em cima da tabela que mais cresce.
const maxLimit = 200

// EventRepo é a única coisa que fala com a tabela events. Append-only:
// não existe UPDATE nem DELETE aqui, e isso não é esquecimento.
type EventRepo struct{ pool *pgxpool.Pool }

func NewEventRepo(pool *pgxpool.Pool) *EventRepo { return &EventRepo{pool: pool} }

// payload é a forma no JSONB. snake_case porque é o mesmo shape que sai no
// wire (api-contract), e omitempty porque evento de sono não tem zona.
type payload struct {
	Kcal    uint16 `json:"kcal,omitempty"`
	Zone    uint8  `json:"zone,omitempty"`
	Minutes uint16 `json:"minutes,omitempty"`
	PeerID  string `json:"peer_id,omitempty"`
}

// O lote inteiro num round-trip só. unnest transforma cinco arrays em N linhas,
// então 200 eventos são uma query e uma transação, não 200 idas ao banco.
//
// O cursor sai da MESMA query de propósito: pedir num SELECT separado abriria
// janela pra outro lote entrar no meio e o device receber um cursor que já
// nasceu velho.
//
// GREATEST entre os dois lados porque um CTE que insere não é visível pro
// SELECT irmão (mesma statement, mesmo snapshot): `ins` traz o que acabou de
// entrar, `events` traz o que já estava lá, e o cursor é o maior dos dois.
// Devolve os event_id que REALMENTE entraram, não só a contagem. A diferença
// importa: quem chama precisa distinguir "evento novo e atrasado", que obriga
// replay do snapshot, de "evento duplicado", que já está foldado e não obriga
// nada. Sem essa lista, todo retry de lote viraria um replay do log inteiro.
const qAppend = `
WITH ins AS (
    INSERT INTO events (event_id, pet_id, occurred_at, kind, payload)
    SELECT u.event_id, $1, u.occurred_at, u.kind, u.payload
    FROM unnest($2::text[], $3::timestamptz[], $4::smallint[], $5::jsonb[])
         AS u(event_id, occurred_at, kind, payload)
    ON CONFLICT (pet_id, event_id) DO NOTHING
    RETURNING seq, event_id
)
SELECT
    COALESCE((SELECT array_agg(event_id) FROM ins), '{}')::text[],
    GREATEST(
        COALESCE((SELECT max(seq) FROM ins), 0),
        COALESCE((SELECT max(seq) FROM events WHERE pet_id = $1), 0)
    )`

// Append grava o lote e devolve os IDs que entraram de fato mais o cursor do
// pet. Reenviar o mesmo lote devolve lista vazia e nenhum erro: duplicata é o
// caminho feliz de um retry, não uma falha.
func (r *EventRepo) Append(ctx context.Context, petID string, evs []sim.Event) ([]string, int64, error) {
	if len(evs) == 0 {
		cursor, err := r.cursor(ctx, petID)
		return nil, cursor, err
	}

	ids := make([]string, len(evs))
	ats := make([]time.Time, len(evs))
	kinds := make([]int16, len(evs))
	loads := make([]string, len(evs))

	for i, ev := range evs {
		b, err := json.Marshal(payload{Kcal: ev.Kcal, Zone: ev.Zone, Minutes: ev.Minutes, PeerID: ev.PeerID})
		if err != nil {
			return nil, 0, fmt.Errorf("serializar payload de %s: %w", ev.ID, err)
		}
		ids[i] = ev.ID
		ats[i] = ev.At.UTC()
		kinds[i] = int16(ev.Kind)
		loads[i] = string(b)
	}

	var novos []string
	var cursor int64
	err := r.pool.QueryRow(ctx, qAppend, petID, ids, ats, kinds, loads).Scan(&novos, &cursor)
	if err != nil {
		return nil, 0, fmt.Errorf("append de %d eventos do pet %s: %w", len(evs), petID, err)
	}
	return novos, cursor, nil
}

const qSince = `
SELECT seq, event_id, occurred_at, kind, payload
FROM events
WHERE pet_id = $1 AND seq > $2
ORDER BY seq
LIMIT $3`

// Since devolve os eventos com seq maior que o cursor, em ordem de seq, mais
// o seq da última linha da página. Cursor monotônico, nunca offset: offset
// pula e repete quando entra linha nova no meio da paginação.
//
// O seq devolvido é o da última linha LIDA, não o maior seq do pet. Ele vira
// folded_seq no snapshot, e marcar como foldado um evento que ainda não foi
// lido perde evento em silêncio.
//
// Página vazia devolve o cursor de entrada intacto, pra que o chamador possa
// parar o laço sem tratar caso especial.
func (r *EventRepo) Since(ctx context.Context, petID string, cursor int64, limit int) ([]sim.Event, int64, error) {
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}

	rows, err := r.pool.Query(ctx, qSince, petID, cursor, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("ler eventos do pet %s: %w", petID, err)
	}
	defer rows.Close()

	ultimo := cursor
	evs := make([]sim.Event, 0, limit)
	for rows.Next() {
		var (
			ev   sim.Event
			seq  int64
			kind int16
			raw  []byte
		)
		if err := rows.Scan(&seq, &ev.ID, &ev.At, &kind, &raw); err != nil {
			return nil, 0, fmt.Errorf("scan de evento do pet %s: %w", petID, err)
		}
		var p payload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, 0, fmt.Errorf("payload do evento %s: %w", ev.ID, err)
		}
		ev.At = ev.At.UTC()
		ev.Kind = sim.Kind(kind)
		ev.Kcal, ev.Zone, ev.Minutes, ev.PeerID = p.Kcal, p.Zone, p.Minutes, p.PeerID
		evs = append(evs, ev)
		ultimo = seq
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ler eventos do pet %s: %w", petID, err)
	}
	return evs, ultimo, nil
}

func (r *EventRepo) cursor(ctx context.Context, petID string) (int64, error) {
	var seq int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(max(seq), 0) FROM events WHERE pet_id = $1`, petID).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("cursor do pet %s: %w", petID, err)
	}
	return seq, nil
}

// FirstAt devolve o instante do evento cronologicamente mais antigo do pet,
// que é a definição operacional de quando o bicho nasceu.
//
// ORDER BY occurred_at, não por seq: seq é ordem de inserção, e device offline
// empurra passado. Ancorar o genesis no primeiro INSERIDO faria o Fold
// descartar todo evento que aconteceu antes dele.
func (r *EventRepo) FirstAt(ctx context.Context, petID string) (time.Time, bool, error) {
	var at time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT occurred_at FROM events WHERE pet_id = $1 ORDER BY occurred_at LIMIT 1`,
		petID).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("primeiro evento do pet %s: %w", petID, err)
	}
	return at.UTC(), true, nil
}

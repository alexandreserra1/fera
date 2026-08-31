package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ale/fera/internal/sim"
)

// SnapshotRepo guarda o cache materializado do fold. Tudo aqui é descartável
// por construção: se sumir, o próximo Get refolda a partir do log.
type SnapshotRepo struct{ pool *pgxpool.Pool }

func NewSnapshotRepo(pool *pgxpool.Pool) *SnapshotRepo { return &SnapshotRepo{pool: pool} }

// Filtra por schema_ver na própria query: snapshot gerado por uma versão
// anterior das regras não é dado velho, é dado errado. Devolver ok=false faz
// o chamador refoldar do log, que é a resposta certa e barata.
const qLoadSnapshot = `
SELECT state, folded_seq
FROM pet_snapshots
WHERE pet_id = $1 AND schema_ver = $2`

// Load devolve ok=false quando não há snapshot utilizável. Ausência não é
// erro: é o estado normal de um pet novo e de um pet cujo schema mudou.
func (r *SnapshotRepo) Load(ctx context.Context, petID string) (sim.State, int64, bool, error) {
	var (
		raw []byte
		seq int64
	)
	err := r.pool.QueryRow(ctx, qLoadSnapshot, petID, sim.SchemaVer).Scan(&raw, &seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return sim.State{}, 0, false, nil
	}
	if err != nil {
		return sim.State{}, 0, false, fmt.Errorf("carregar snapshot do pet %s: %w", petID, err)
	}

	var s sim.State
	if err := json.Unmarshal(raw, &s); err != nil {
		return sim.State{}, 0, false, fmt.Errorf("decodificar snapshot do pet %s: %w", petID, err)
	}
	return s, seq, true, nil
}

// O WHERE no DO UPDATE é o que impede um worker atrasado de fazer o snapshot
// andar pra trás. Escrita fora de ordem vira no-op silencioso, que é o
// comportamento certo: quem chegou atrasado não tem nada a acrescentar.
//
// A segunda condição não é enfeite. Sem ela, uma linha de schema antigo com
// folded_seq alto trava a chave pra sempre: o Load a ignora por schema, e todo
// Save novo (que começa de folded_seq baixo, porque refoldou do genesis) é
// bloqueado pelo folded_seq maior da linha morta. O pet passa a refoldar o log
// inteiro em TODA leitura, sem erro nenhum aparecendo em lugar nenhum.
const qSaveSnapshot = `
INSERT INTO pet_snapshots (pet_id, state, folded_seq, folded_at, schema_ver)
VALUES ($1, $2, $3, now(), $4)
ON CONFLICT (pet_id) DO UPDATE
SET state = EXCLUDED.state, folded_seq = EXCLUDED.folded_seq,
    folded_at = EXCLUDED.folded_at, schema_ver = EXCLUDED.schema_ver
WHERE pet_snapshots.folded_seq < EXCLUDED.folded_seq
   OR pet_snapshots.schema_ver <> EXCLUDED.schema_ver`

func (r *SnapshotRepo) Save(ctx context.Context, petID string, s sim.State, seq int64) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("serializar snapshot do pet %s: %w", petID, err)
	}
	// grava o SchemaVer do próprio State, não a constante do binário: a coluna
	// descreve a LINHA. Fold sempre carimba s.SchemaVer, então em operação
	// normal os dois são iguais, e essa é justamente a razão de não haver
	// motivo pros dois poderem discordar.
	if _, err := r.pool.Exec(ctx, qSaveSnapshot, petID, raw, seq, s.SchemaVer); err != nil {
		return fmt.Errorf("salvar snapshot do pet %s: %w", petID, err)
	}
	return nil
}

// Delete joga o snapshot fora pra forçar replay do genesis no próximo Get.
// É o que acontece quando chega evento anterior ao último já foldado: o fold
// incremental descartaria esse evento, então o caminho certo é refazer tudo.
//
// Apagar o que não existe não é erro: o chamador não deve precisar checar
// antes, e checar antes seria uma corrida de qualquer jeito.
func (r *SnapshotRepo) Delete(ctx context.Context, petID string) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM pet_snapshots WHERE pet_id = $1`, petID); err != nil {
		return fmt.Errorf("apagar snapshot do pet %s: %w", petID, err)
	}
	return nil
}

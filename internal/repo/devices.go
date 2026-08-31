package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeviceRepo struct{ pool *pgxpool.Pool }

func NewDeviceRepo(pool *pgxpool.Pool) *DeviceRepo { return &DeviceRepo{pool: pool} }

func (r *DeviceRepo) Create(ctx context.Context, deviceID, petID string, tokenHash, signKey []byte, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
        INSERT INTO devices (device_id, pet_id, token_hash, sign_key, last_seen, last_cursor)
        VALUES ($1, $2, $3, $4, $5, 0)`,
		deviceID, petID, tokenHash, signKey, now)
	if err != nil {
		return fmt.Errorf("registrar device do pet %s: %w", petID, err)
	}
	return nil
}

// ByTokenHash resolve o device a partir do hash do token apresentado.
//
// Lookup por índice único no hash. É isso que o SHA-256 sem salt compra: hash
// salgado não é pesquisável, e resolver token viraria varredura da tabela.
//
// Não devolve erro pra token desconhecido: quem não achou nada não errou, só
// não está autenticado, e a borda é que decide o que isso significa.
func (r *DeviceRepo) ByTokenHash(ctx context.Context, tokenHash []byte) (string, string, bool, error) {
	var deviceID, petID string
	err := r.pool.QueryRow(ctx,
		`SELECT device_id, pet_id FROM devices WHERE token_hash = $1`,
		tokenHash).Scan(&deviceID, &petID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("resolver device por token: %w", err)
	}
	return deviceID, petID, true, nil
}

// ByID resolve o device pelo id, pra verificar assinatura.
//
// O caminho assinado precisa achar a chave ANTES de conferir o MAC, e o id vem
// em header. Isso é diferente do Bearer, onde o próprio segredo é a busca.
// Consequência: um id existente e um inexistente têm que dar a mesma resposta
// pra quem está sondando, e quem garante isso é a borda.
func (r *DeviceRepo) ByID(ctx context.Context, deviceID string) (string, []byte, bool, error) {
	var petID string
	var chave []byte
	err := r.pool.QueryRow(ctx,
		`SELECT pet_id, sign_key FROM devices WHERE device_id = $1`,
		deviceID).Scan(&petID, &chave)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("resolver device %s: %w", deviceID, err)
	}
	if len(chave) == 0 {
		return "", nil, false, nil // device antigo, só Bearer
	}
	return petID, chave, true, nil
}

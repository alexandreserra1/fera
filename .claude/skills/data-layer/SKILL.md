---
name: data-layer
description: Regras de banco e persistência da FERA. Carregue ao escrever SQL, migração, repositório pgx, ou ao decidir como consultar dados. Cobre a proibição de joins, event sourcing, idempotência via constraint e reuso de conexão.
---

# Camada de dados

## Regra 1: sem JOIN em caminho quente

Se você precisou de join, a modelagem está errada ou você precisa de uma projeção.
Projeção = tabela nova, populada por consumer do log de eventos, otimizada
pra uma leitura específica. Nunca resolva relação em query de request.

## Regra 2: idempotência é constraint, não código

```sql
PRIMARY KEY (pet_id, event_id)
```

```go
const qAppend = `
INSERT INTO events (event_id, pet_id, occurred_at, kind, payload)
SELECT * FROM unnest($1::text[], $2::uuid[], $3::timestamptz[], $4::smallint[], $5::jsonb[])
ON CONFLICT (pet_id, event_id) DO NOTHING`
```

Lote inteiro num round-trip com `unnest`. Não faça loop de INSERT.
Não use `pgx.Batch` pra isso. `unnest` é uma query só, uma transação só.

`CommandTag.RowsAffected()` te dá quantos foram novos. A diferença são duplicatas.

## Regra 3: nunca UPDATE em `events`

Append-only. Se um evento estava errado, emita um evento de compensação.
Isso é o que dá auditabilidade e replay.

## Snapshot

```go
func (r *SnapshotRepo) Save(ctx context.Context, petID string, s sim.State, seq int64) error {
    _, err := r.pool.Exec(ctx, `
        INSERT INTO pet_snapshots (pet_id, state, folded_seq, folded_at, schema_ver)
        VALUES ($1, $2, $3, now(), $4)
        ON CONFLICT (pet_id) DO UPDATE
        SET state = EXCLUDED.state, folded_seq = EXCLUDED.folded_seq,
            folded_at = EXCLUDED.folded_at, schema_ver = EXCLUDED.schema_ver
        WHERE pet_snapshots.folded_seq < EXCLUDED.folded_seq`,
        petID, s, seq, sim.SchemaVer)
    return err
}
```

O `WHERE folded_seq < EXCLUDED.folded_seq` impede que um worker atrasado
sobrescreva um snapshot mais novo. Escrita fora de ordem vira no-op.

## Pool de conexões

```go
cfg, _ := pgxpool.ParseConfig(dsn)
cfg.MaxConns = int32(runtime.NumCPU() * 4)
cfg.MinConns = 2
cfg.MaxConnLifetime = 30 * time.Minute
cfg.MaxConnIdleTime = 5 * time.Minute
cfg.HealthCheckPeriod = 1 * time.Minute
pool, err := pgxpool.NewWithConfig(ctx, cfg)
```

- Um pool no processo inteiro, criado no `main`, passado por injeção.
- `MaxConnLifetime` menor que qualquer timeout de proxy no meio.
- Nunca `pool.Acquire` manual a não ser que você precise de sessão sticky.
  E aí sempre com `defer conn.Release()`.

## Particionamento

`events` particionada por `RANGE (received_at)`, mensal.
Job cria a partição do próximo mês com antecedência. Partição sem criar
= INSERT falhando às 00:00 do dia 1. Já aconteceu com todo mundo.

## Migrações

Numeradas, sequenciais, **sempre reversíveis ou explicitamente irreversíveis**.
Ferramenta: `tern` (mesmo autor do pgx) ou `goose`. Sem migração automática no boot.

## Testes de repositório

`testcontainers-go` com Postgres real. Um container por pacote de teste,
não por teste. Migrações rodam no setup do container.
Nunca mocke `pgx`, você estaria testando o mock.

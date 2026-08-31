# Modelo de dados

Requisito seu: **sem joins**. Aqui isso não é gambiarra, é consequência do event sourcing.

## Tabelas (são duas, mais uma de identidade)

### `events` — append-only, nunca UPDATE, nunca DELETE

```sql
CREATE TABLE events (
    seq         BIGSERIAL,
    event_id    TEXT        NOT NULL,   -- ULID gerado no CLIENTE
    pet_id      UUID        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,   -- relógio do cliente
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind        SMALLINT    NOT NULL,
    payload     JSONB       NOT NULL,
    PRIMARY KEY (pet_id, event_id)      -- <- a idempotência mora aqui
);

CREATE UNIQUE INDEX events_pet_seq_idx ON events (pet_id, seq);
```

Ver `migrations/0001_init.sql`.

Insert:
```sql
INSERT INTO events (...) VALUES (...) ON CONFLICT (pet_id, event_id) DO NOTHING;
```

Reenviou o mesmo lote 40 vezes? Nada acontece. Idempotência resolvida em uma linha de SQL,
sem tabela de idempotency key, sem Redis, sem TTL pra gerenciar. Vale inclusive pra
duplicata dentro do próprio lote: `ON CONFLICT DO NOTHING` resolve conflito contra
linha que a mesma statement acabou de inserir. Coberto por
`TestAppendLoteComDuplicataInterna`.

## Por que a tabela NÃO é particionada (correção da v1 deste doc)

A versão original pedia `PARTITION BY RANGE (received_at)` junto com
`PRIMARY KEY (pet_id, event_id)`. Isso não roda:

```
ERROR:  unique constraint on partitioned table must include all partitioning columns
DETAIL: PRIMARY KEY constraint on table "events" lacks column "received_at"
        which is part of the partition key.
```

E o conserto óbvio é pior que o erro. Botar `received_at` na PK faz o DDL rodar
e quebra o invariante 3 **em silêncio**: `received_at` é `DEFAULT now()`, muda a
cada reenvio, e o mesmo evento vira duas linhas sem nenhum erro aparecer.
Medido num Postgres 16: dois `INSERT ... ON CONFLICT DO NOTHING` do mesmo ULID
resultaram em 2 linhas.

Idempotência é invariante, particionamento é otimização, e otimizar uma tabela
com zero linhas é montar máquina pra problema que não existe. Fica sem partição
até haver volume que justifique escolher o esquema com dado real na mão.

Quando chegar lá, as opções reais, nesta ordem:

| Esquema | PK continua válida? | O que ganha | O que perde |
|---|---|---|---|
| `HASH (pet_id)` | sim, `pet_id` é a chave de partição | pruning perfeito, já que toda query quente filtra por `pet_id` | arquivamento por tempo: evento velho sai por `DELETE`, não `DETACH` |
| `RANGE (occurred_at)` | só como `(pet_id, event_id, occurred_at)` | mantém o `DETACH PARTITION` mensal | cliente que mandar o mesmo `event_id` com `occurred_at` diferente cria duplicata |
| `RANGE (received_at)` | **não** | nada | quebra a idempotência. Não use. |

### `pet_snapshots` — cache materializado do fold

```sql
CREATE TABLE pet_snapshots (
    pet_id       UUID PRIMARY KEY,
    state        JSONB       NOT NULL,
    folded_seq   BIGINT      NOT NULL,  -- último seq incluído
    folded_at    TIMESTAMPTZ NOT NULL,
    schema_ver   SMALLINT    NOT NULL
);
```

Leitura quente = `SELECT state FROM pet_snapshots WHERE pet_id = $1`.
Um índice, uma linha, zero join. Cache do otter na frente disso.

Snapshot é **descartável**. Se `schema_ver` mudar (você corrigiu uma regra do sim),
apaga tudo e recalcula. Isso é recuperabilidade real, não backup de sexta-feira.

### `devices`

```sql
CREATE TABLE devices (
    device_id   UUID PRIMARY KEY,
    pet_id      UUID NOT NULL,
    pubkey      BYTEA NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL,
    last_cursor BIGINT NOT NULL DEFAULT 0
);
```

## Por que não tem join

Porque o único dado relacional aqui é "eventos deste pet", e isso é filtro
por chave, não join. Se um dia você precisar de ranking global ou feed social,
isso vira uma **projeção separada** (outra tabela, populada por consumer do log),
nunca um join em query quente. Essa é a regra: leitura nova = projeção nova.

## Conexões

- `pgxpool` com `MaxConns` = 4x núcleos, `MinConns` = 2, `MaxConnLifetime` 30min.
- Nunca `sql.Open` por request. Pool é global, criado no `main`, injetado.
- `MaxConnIdleTime` menor que o timeout do Postgres, senão você pega conexão morta.
- HTTP de saída: um `*http.Client` global com `Transport` configurado.
  `http.DefaultClient` não reusa direito sob carga e não tem timeout.
- No device: uma conexão TLS por sessão de sync, reaproveitada pro batch inteiro.

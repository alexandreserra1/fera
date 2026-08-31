-- 0001: o modelo de dados inteiro. Duas tabelas de verdade e uma de identidade.
--
-- SOBRE PARTICIONAMENTO (decisão que contraria o docs/02 original):
-- o docs/02 pedia PARTITION BY RANGE (received_at) junto com
-- PRIMARY KEY (pet_id, event_id). O Postgres recusa: toda constraint única em
-- tabela particionada precisa conter a chave de partição. E o conserto óbvio,
-- botar received_at na PK, quebra o invariante 3 em silêncio: received_at é
-- DEFAULT now(), muda a cada reenvio, e o mesmo evento vira duas linhas sem
-- nenhum erro aparecer.
--
-- Idempotência é o invariante, particionamento é otimização. Fica sem partição
-- até existir volume que justifique escolher um esquema com dado real na mão.
-- Quando chegar lá: HASH (pet_id) preserva esta PK e casa com o fato de que
-- toda query quente filtra por pet_id.

CREATE TABLE events (
    seq         BIGSERIAL,
    event_id    TEXT        NOT NULL,   -- ULID gerado no CLIENTE
    pet_id      UUID        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,   -- relógio do cliente
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind        SMALLINT    NOT NULL,
    payload     JSONB       NOT NULL,

    -- a idempotência mora aqui, e em nenhum outro lugar. Sem tabela de
    -- idempotency key, sem Redis, sem TTL pra gerenciar.
    PRIMARY KEY (pet_id, event_id)
);

-- o índice do cursor: (pet_id, seq) serve tanto o pull incremental quanto o
-- max(seq) que vira cursor na resposta do ingest
CREATE UNIQUE INDEX events_pet_seq_idx ON events (pet_id, seq);

CREATE TABLE pet_snapshots (
    pet_id      UUID PRIMARY KEY,
    state       JSONB       NOT NULL,
    folded_seq  BIGINT      NOT NULL,   -- último seq incluído no fold
    folded_at   TIMESTAMPTZ NOT NULL,
    schema_ver  SMALLINT    NOT NULL
);

CREATE TABLE devices (
    device_id   UUID PRIMARY KEY,
    pet_id      UUID NOT NULL,
    pubkey      BYTEA NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL,
    last_cursor BIGINT NOT NULL DEFAULT 0
);

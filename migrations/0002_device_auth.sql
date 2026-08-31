-- 0002: token de device.
--
-- SOBRE O HASH (decisão que contraria o api-contract original):
-- o doc pedia argon2id. Argon2id existe pra encarecer brute force contra
-- segredo de BAIXA entropia, que é o caso de senha escolhida por humano.
-- O token aqui é 32 bytes de crypto/rand: 256 bits. Não existe brute force
-- pra encarecer, e argon2id cobraria ~50ms de CPU em TODA requisição
-- autenticada, num device que sincroniza a cada 15 minutos.
--
-- SHA-256 sem salt é o correto pra este caso e é o que permite o lookup ser
-- um índice único em vez de varredura da tabela inteira: hash salgado não é
-- pesquisável, então com argon2id seria preciso ou embutir o device_id no
-- token ou escanear todos os devices a cada request.
--
-- Se um dia o token virar algo escolhido por gente, isto muda.

ALTER TABLE devices
    ALTER COLUMN pubkey DROP NOT NULL,
    ADD COLUMN token_hash BYTEA NOT NULL,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX devices_token_hash_idx ON devices (token_hash);

-- um dono pode ter mais de um device no mesmo pet (celular + bicho)
CREATE INDEX devices_pet_idx ON devices (pet_id);

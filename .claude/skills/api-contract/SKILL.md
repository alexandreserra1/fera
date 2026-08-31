---
name: api-contract
description: Contrato HTTP da FERA. Carregue ao criar ou alterar endpoint, DTO, código de erro, header de auth, versionamento ou paginação. Define o formato de resposta e as regras de idempotência na borda.
---

# Contrato de API

## Convenções

- Prefixo `/v1`. Versão na URL, não em header. Simples ganha.
- JSON, `snake_case` no wire, `CamelCase` no Go.
- Timestamps sempre RFC3339 em UTC.
- Paginação sempre por **cursor** (`?since=<seq>&limit=`), nunca offset.
- DTO nunca é o tipo do domínio. `internal/http/dto.go` tem `toDomain()` / `fromDomain()`.

## Endpoints

```
POST   /v1/devices/register        registra device + pet, devolve token (único aberto)
POST   /v1/pets                    cria pet (genesis)
GET    /v1/pets/{id}               estado atual (snapshot + fold até now)
POST   /v1/pets/{id}/events        ingest de lote (idempotente)
GET    /v1/pets/{id}/events        pull por cursor
GET    /v1/pets/{id}/timeline      projeção de histórico legível
GET    /healthz                    liveness, sem tocar no banco
GET    /readyz                     readiness, ping no pool
```

Sete endpoints. Se a lista crescer pra 30, alguma coisa deu errado.

## Formato de erro, único

```json
{"error": {"code": "invalid_event", "message": "kind desconhecido: 99"}}
```

`code` é estável e o cliente pode programar em cima. `message` é pra humano
e pode mudar. Nunca coloque stack trace, nome de tabela ou erro de driver aqui.

## Idempotência

O corpo carrega os ULIDs. **Não use header `Idempotency-Key`** neste projeto:
o lote tem N eventos com identidades próprias, uma chave de request não serve.

Resposta sempre informa o que aconteceu:
```json
{"accepted": 18, "duplicates": 2, "rejected": 0, "cursor": 91841}
```

Reenviar o mesmo lote devolve `{"accepted": 0, "duplicates": 20}` e status 200.
Nunca 409 pra duplicata em ingest. Duplicata é o caminho feliz de um retry.

## Auth

Token opaco por device, emitido no register, guardado **hasheado com SHA-256**.
`Authorization: Bearer <token>`. Middleware resolve device e injeta no contexto
via chave tipada:

```go
type ctxKey int
const deviceKey ctxKey = iota
```

Nunca `context.WithValue(ctx, "device", d)` com string. Colide.

### Por que SHA-256 e não argon2id (correção da v1 deste doc)

Argon2id existe pra encarecer brute force contra segredo de **baixa entropia**,
que é o caso de senha escolhida por humano. O token daqui é 32 bytes de
`crypto/rand`: 256 bits. Não há brute force pra encarecer, e argon2id cobraria
~50 ms de CPU em toda requisição autenticada.

Tem uma segunda razão, estrutural: hash salgado **não é pesquisável**. Com
argon2id o lookup viraria varredura da tabela de devices a cada request, ou
exigiria embutir o `device_id` no token pra achar a linha antes de verificar.
SHA-256 sem salt (seguro aqui justamente porque o segredo é aleatório e longo)
permite `UNIQUE INDEX` no hash e resolve em uma busca por índice.

Se um dia o segredo virar algo escolhido por gente, isto muda.

### Autenticação e autorização são coisas separadas

`requireDevice` diz **quem** é. `requireOwnPet` diz se esse quem pode mexer
**neste** pet. Sem o segundo, qualquer device registrado lê e escreve no log de
todo mundo, porque ter token válido não é ter direito a um `pet_id` qualquer.

O `pet_id` é gerado no servidor no register e nunca aceito do corpo. Se o
cliente escolhesse, se registraria no pet de outro e a autorização não
significaria nada.

Pet de outro dono devolve **404, não 403**: 403 confirma que aquele `pet_id`
existe, e `pet_id` é exatamente o que não se deve conseguir sondar. Pelo mesmo
motivo, token ausente, torto e desconhecido dão os três a mesma resposta.

## Validação

Na borda, no DTO, antes de virar domínio. Sem lib de validação por tag.
Uma função `func (r ingestRequest) validate() error` explícita.
Limites: 200 eventos por lote, payload de 1MB, `occurred_at` no máximo
24h no futuro (clampa) e 90 dias no passado (rejeita).

## Contrato como teste

`internal/http/testdata/` guarda pares request/response em JSON.
Teste roda o servidor com `httptest`, dispara os arquivos e compara.
Isso vira documentação executável e pega quebra de contrato no CI.

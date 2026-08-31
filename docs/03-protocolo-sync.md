# Protocolo de sync

Regra de ouro: **o device nunca pede permissão.** Ele gera eventos, aplica localmente,
e empurra quando tiver rede. O servidor é histórico e backup, não autoridade em tempo real.

## Push (device → servidor)

```
POST /v1/pets/{pet_id}/events
Authorization: Bearer <device token>
Content-Type: application/json

{
  "events": [
    {"id":"01J...", "kind":"effort", "at":"2026-08-22T10:00:00Z", "payload":{"kcal":420,"zone":3}},
    {"id":"01J...", "kind":"sleep",  "at":"2026-08-22T06:00:00Z", "payload":{"minutes":430}}
  ]
}
```

Resposta:
```json
{"accepted": 2, "duplicates": 0, "cursor": 91823}
```

- Lote de até 200 eventos. Se falhar, o device reenvia o lote inteiro. É seguro.
- `id` é ULID gerado no device. Nunca no servidor. Nunca autoincrement.
- Ordem no lote não importa.

## Pull (servidor → device / app)

```
GET /v1/pets/{pet_id}/events?since=91823&limit=200
```

Cursor monotônico em `seq`. O cliente guarda o último cursor visto e continua daí.
Sem paginação por offset. Sem timestamp como cursor (relógio mente).

**O cursor tem buracos, e está tudo certo.** `BIGSERIAL` consome número nas
tentativas que o `ON CONFLICT` descarta, então reenviar um lote de 3 eventos
avança o cursor em 3 sem inserir nada. Ele só precisa ser monotônico, não denso.
Cliente que interpretar buraco como evento perdido está lendo errado.

## Reconciliação

Device e servidor rodam o **mesmo** `sim.Fold`. Depois do sync, os dois convergem
no mesmo estado, sem negociação. Não há merge, não há CRDT, não há conflito.
Isso só funciona porque `Fold` é determinístico. Qualquer coisa não determinística
no core (map iteration order, `time.Now()`, `rand` sem seed) **quebra o sistema inteiro**.

### Evento atrasado e a regra de replay

`Fold` aplica evento cujo `At` seja posterior ao último evento já aplicado
(`State.LastAtUnix`), desempatando por ULID no mesmo segundo. Isso cobre o caso
comum: device offline por dias empurrando um lote inteiro fora de ordem.

O que `Fold` **não** faz é reescrever a história. Evento que chega com `At`
anterior a um evento já aplicado é ignorado, porque aplicá-lo no lugar certo
exigiria refazer todo o decaimento a partir dali. A regra fica no
`internal/service`:

> se o lote traz algum evento **novo** com `At` menor que o `LastAtUnix` do
> snapshot, o snapshot é invalidado e o pet refolda a partir do genesis.

A palavra "novo" carrega peso. Duplicado nunca aplicaria sobre o snapshot,
porque já está foldado dentro dele. Se a regra olhasse o lote inteiro, todo
reenvio — o caminho feliz de um retry — custaria um replay do log completo, e
o caminho mais comum do protocolo viraria o mais caro do sistema. Por isso o
`Append` devolve **quais** IDs entraram, não só quantos.

Insere primeiro, decide depois: o log é a verdade e já tem o evento. Refoldar
é barato porque o snapshot é descartável por construção.

## Relógio

Device sem RTC vai mentir depois de um reboot. Solução:

- Device guarda `monotonic_ticks` desde boot e um `wall_clock_anchor` do último sync.
- Evento carrega os dois. Servidor corrige `occurred_at` se detectar drift absurdo
  (mais de 24h à frente de `received_at` → clampa).
- ULID já embute timestamp, o que dá ordenação estável mesmo com relógio ruim.

Dentro do `sim` o tempo é **unix em segundos**, e a ordenação é por
`(segundo, ULID)`. Duas coisas caem disso: precisão de sub-segundo não influencia
o resultado (o ULID desempata), e o `State` é comparável com `==` e sobrevive a
round-trip por JSONB sem surpresa de location ou relógio monotônico.

## BLE peer-to-peer

Dois devices próximos trocam um "encontro":

1. Advertise: `pet_id` curto + hash do estado + versão do protocolo.
2. Se ambos aceitam, cada um gera um evento `encounter` com o mesmo `encounter_nonce`.
3. Cada device aplica o encontro localmente e sincroniza depois.

O `encounter_nonce` compartilhado é o que garante que o servidor não conte o encontro
duas vezes quando os dois donos sincronizarem. Idempotência de novo, mesma ferramenta.

Rate limit: no máximo 1 encontro válido por par de pets a cada 6 horas. Sem isso,
duas FERAs na mesma casa farmam vínculo infinito. Regra fica **dentro do sim**,
não no servidor, senão o device offline burla.

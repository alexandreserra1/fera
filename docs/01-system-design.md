# System Design

## Princípio central: event sourcing determinístico

O estado da FERA **não é armazenado como verdade**. A verdade é o log de eventos.

```
estado   = Fold(genesis, eventos)          <- puro, para no último evento, é o que se persiste
visão(t) = Project(estado, t)              <- decaimento do tempo parado, só pra tela
```

`Fold` é puro. Mesma entrada, mesma saída, em qualquer máquina, sempre.

As duas funções são separadas de propósito, e `Project` devolve um tipo
diferente (`sim.View`, não `sim.State`) justamente pra que ninguém consiga
persistir a projeção. Se a projeção virasse snapshot, o `LastAtUnix` dela seria
"agora", e todo evento chegando depois com `At` anterior a esse instante seria
descartado em silêncio. Que é exatamente o caminho normal de um device offline
empurrando lote atrasado.

Isso resolve de uma vez, sem esforço extra, tudo que você pediu:

| Requisito | Como cai de graça |
|---|---|
| Idempotência | Cada evento carrega um ULID gerado no cliente. Insert com `ON CONFLICT DO NOTHING`. Reenvio é no-op. |
| Recuperabilidade | Perdeu o snapshot? Replay do log. Bug no fold? Corrige e replay. |
| Escalabilidade | Escrita é append-only. Leitura é 1 linha de snapshot. Nada trava. |
| Confiabilidade | Device funciona offline e empurra o log depois. Ordem não importa (fold ordena). |
| Sem joins | Duas tabelas, nenhuma relação a resolver em query quente. |
| Reutilização | Um `Fold`, três runtimes, travado por golden vectors. |

## Diagrama

```
        ┌───────────────┐   BLE   ┌───────────────┐
        │  FERA device  │◄───────►│  FERA device  │   (peer-to-peer, sem servidor)
        │  ESP32-S3     │         │   do amigo    │
        │ Fold/Project  │         └───────────────┘
        └───────┬───────┘
                │ HTTPS, batch de eventos, offline-first
                ▼
      ┌─────────────────────┐
      │  Caddy (TLS, rate)  │  <- seu "api gateway". Não use Kong.
      └──────────┬──────────┘
                 ▼
      ┌─────────────────────────────────┐
      │  fera-api (binário Go único)    │
      │  chi + middlewares              │
      │  ┌───────────────────────────┐  │
      │  │ handler → service → repo  │  │
      │  └───────────────────────────┘  │
      │  otter (cache) + singleflight   │
      │  sim.Fold/Project (mesmo core)  │
      └──────────┬──────────────────────┘
                 │ pgxpool (conexões reutilizadas)
                 ▼
      ┌─────────────────────────────────┐
      │  PostgreSQL                     │
      │  events (append-only, partido)  │
      │  pet_snapshots (JSONB)          │
      └─────────────────────────────────┘
                 ▲
                 │ HTTPS
      ┌──────────┴──────────┐
      │  App Expo (iOS,     │  sim compilado em WASM
      │  Android, web)      │
      └─────────────────────┘
```

## Sobre "API Gateway"

Você pediu. A resposta honesta: **não coloque um API gateway de verdade neste projeto.**
Kong, Tyk ou APISIX resolvem problemas que você não tem (dezenas de serviços,
times separados, políticas por consumidor).

O que você realmente precisa dessa camada:

- TLS e HTTP/2 → **Caddy** (cert automático, 1 arquivo de config)
- rate limit → middleware no Go, com `golang.org/x/time/rate` por device_id
- auth → token de device assinado, validado em middleware
- observabilidade → OpenTelemetry no próprio binário

Tudo isso cabe em ~150 linhas de middleware. Se um dia virar 5 serviços,
aí sim se conversa sobre gateway. Documentar essa decisão vale mais numa entrevista
do que ter subido um Kong sem motivo.

## Camadas do backend

Quatro camadas, dependência sempre pra dentro:

```
cmd/api            main, wiring, graceful shutdown
internal/http      handlers, DTOs, middlewares. Não conhece SQL.
internal/service   casos de uso. Depende de INTERFACES, não de implementações.
internal/repo      pgx. Implementa as interfaces do service.
internal/sim       núcleo puro. Não depende de NADA.
```

Regra: `sim` não importa nada além da stdlib. `service` não importa `pgx`.
Se você precisar de um mock, a interface está no pacote que **consome**, não no que implementa.

## Onde ficam as interfaces (isso importa)

Errado (Java-brain):
```go
// internal/repo/interface.go
type PetRepository interface { ... }  // 12 métodos, um por query
```

Certo (Go-brain):
```go
// internal/service/pet.go
type eventStore interface {
    Append(ctx context.Context, e []sim.Event) error
    Since(ctx context.Context, petID string, cursor int64) ([]sim.Event, error)
}
```

Interface pequena, no consumidor, minúscula, não exportada quando possível.
Se uma interface tem mais de 4 métodos, provavelmente está errada.

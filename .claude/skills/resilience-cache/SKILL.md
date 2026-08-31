---
name: resilience-cache
description: Cache, resiliência e reuso de conexão na FERA. Carregue ao implementar cache, retry, timeout, rate limit, circuit breaker, ou ao configurar clientes HTTP de saída. Lista as libs Go escolhidas e por quê.
---

# Cache e resiliência

## Cache: in-process, não Redis

Este sistema não precisa de Redis. Leitura quente é uma linha de snapshot,
e um binário só serve tudo. Cache distribuído aqui é complexidade sem retorno.

**Lib escolhida: `github.com/maypok86/otter/v2`**

Por quê, em vez das alternativas:

| Lib | Situação |
|---|---|
| `otter/v2` | Baseado em pesquisa recente de cache concorrente, inspirado no Caffeine. Genéricos, TTL, eviction por custo, autoconfiguração pelo paralelismo. É a escolha atual. |
| `theine-go` | Excelente, W-TinyLFU adaptativo, usado no Vitess. Alternativa legítima se otter der problema. |
| `ristretto` | Tem histórico conhecido de degradação de hit rate e usa bem mais memória em benchmarks. Não use em projeto novo. |
| `patrickmn/go-cache` | Um `map` com mutex. Serve pra script, não pra isto. |

```go
cache, _ := otter.MustBuilder[string, sim.State](10_000).
    WithTTL(2 * time.Minute).
    Build()
```

TTL curto de propósito. O snapshot muda a cada evento, e cache velho de bicho
é pior que cache miss.

## Stampede: `singleflight`

Cem devices pedindo o mesmo pet frio não podem virar cem folds.

```go
v, err, _ := g.Do(petID, func() (any, error) {
    return s.foldFromStore(ctx, petID)
})
```

`golang.org/x/sync/singleflight`. Está na x/, é estável, use.

## Retry

Só em erro **transitório e idempotente**. Como todo POST daqui é idempotente
por ULID, retry é seguro no cliente. No servidor, retry só em erro de conexão de banco.

```go
// backoff exponencial com jitter. Sem jitter, todo device volta junto.
delay := base * (1 << attempt)
delay += time.Duration(rand.Int63n(int64(delay / 2)))
```

Máximo 3 tentativas. Depois disso, falhe alto e deixe o cliente reagendar.
Lib: `github.com/cenkalti/backoff/v4` se quiser pronto, ou 20 linhas na mão.

## Circuit breaker

`github.com/sony/gobreaker/v2`. Só nas chamadas de saída pra terceiros
(Strava, Health API). Não coloque breaker no seu próprio Postgres, o pool já lida.

## Rate limit

`golang.org/x/time/rate`, um limiter por `device_id`, guardado no otter com TTL.
100 req/min por device é folgado pra um bicho que sincroniza a cada 15min.

## Reuso de conexão HTTP de saída

```go
var httpClient = &http.Client{
    Timeout: 10 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,   // o default de 2 é o gargalo clássico
        IdleConnTimeout:     90 * time.Second,
        ForceAttemptHTTP2:   true,
    },
}
```

Um cliente global. Nunca `http.Client{}` dentro de função, isso cria um
`Transport` novo e vaza conexão. Nunca `http.DefaultClient`, não tem timeout.

E o mais esquecido: **sempre** `io.Copy(io.Discard, resp.Body)` antes de
`resp.Body.Close()`, senão a conexão não volta pro pool.

## Timeout: em camadas

```
device: 30s total
  Caddy: 25s
    handler middleware: 15s
      chamada ao service: 12s
        query no pg: 8s
```

Cada camada de dentro sempre menor que a de fora. Se inverter, você tem
timeout externo estourando enquanto o trabalho interno continua rodando.

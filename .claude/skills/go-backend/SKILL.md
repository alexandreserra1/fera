---
name: go-backend
description: Padrões do backend Go da FERA. Carregue ao criar ou alterar handlers, services, repositórios, middlewares, wiring do main, ou ao decidir onde colocar uma interface. Cobre camadas, erros, contexto e shutdown.
---

# Backend Go

## Layout

```
cmd/api/main.go            wiring, flags, shutdown. Único lugar com globais.
internal/http/             handler, dto, middleware, router
internal/service/          casos de uso + interfaces que ele consome
internal/repo/             pgx, SQL na mão
internal/sim/              núcleo puro (ver skill sim-core)
internal/platform/         cache, telemetria, clock. Técnico, sem regra de negócio.
migrations/                SQL numerado
```

Sem `pkg/`. Sem `models/`. Sem `utils/`.

## Interfaces: no consumidor, pequenas

```go
// internal/service/pet.go
type eventStore interface {
    Append(ctx context.Context, petID string, evs []sim.Event) (int, error)
    Since(ctx context.Context, petID string, cursor int64, limit int) ([]sim.Event, error)
}

type snapshotStore interface {
    Load(ctx context.Context, petID string) (sim.State, int64, error)
    Save(ctx context.Context, petID string, s sim.State, seq int64) error
}

type PetService struct {
    events    eventStore
    snapshots snapshotStore
    clock     func() time.Time   // injetado, testável
}
```

Nunca defina a interface no pacote `repo`. Nunca chame de `IPetRepository`.
Nunca passe de 4 métodos. Se passou, o service está fazendo coisa demais.

## Handler: fino de verdade

Handler faz três coisas: decodifica, chama service, codifica.

```go
func (h *PetHandler) IngestEvents(w http.ResponseWriter, r *http.Request) {
    var req ingestRequest
    if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
        writeErr(w, http.StatusBadRequest, "corpo inválido")
        return
    }
    res, err := h.svc.Ingest(r.Context(), chi.URLParam(r, "petID"), req.toDomain())
    if err != nil {
        writeErr(w, statusFor(err), safeMsg(err))
        return
    }
    writeJSON(w, http.StatusOK, res)
}
```

`io.LimitReader` sempre. Sem ele, um body de 4GB derruba o processo.

## Erros

```go
var (
    ErrNotFound  = errors.New("não encontrado")
    ErrForbidden = errors.New("proibido")
    ErrConflict  = errors.New("conflito")
)
```

Três sentinelas, que é o que o HTTP precisa distinguir. O resto é `%w` com contexto.
`statusFor(err)` usa `errors.Is`. Nunca vaze erro de banco pro cliente.

## Contexto

- Primeiro parâmetro de tudo que faz I/O.
- Timeout por request via middleware: 5s leitura, 15s ingest de lote.
- Nunca guarde `context.Context` em struct.
- `context.Background()` só no `main` e em teste.

## Shutdown gracioso

```go
srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
go func() { _ = srv.ListenAndServe() }()

ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
<-ctx.Done()

shutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
defer cancel()
_ = srv.Shutdown(shutCtx)
pool.Close()
```

`ReadHeaderTimeout` não é opcional. Sem ele é Slowloris na veia.

## Observabilidade

- `log/slog` com handler JSON. Sem logrus, sem zap.
- OpenTelemetry só nos boundaries: HTTP in, SQL out. Não instrumente o `sim`.
- Métricas que importam: eventos aceitos vs duplicados, latência do fold,
  lag entre `seq` do log e `folded_seq` do snapshot.

---
name: tdd-guard
description: Regras de TDD obrigatórias do projeto FERA. Carregue antes de escrever QUALQUER código Go, e antes de responder pedidos de "implementa X", "adiciona feature Y" ou "corrige bug Z". Define o ciclo, os tipos de teste por camada e o que nunca mockar.
---

# TDD como regra, não como sugestão

## O ciclo, sem negociação

1. Escreva o teste. Ele deve **falhar** e você deve mostrar a saída da falha.
2. Escreva o mínimo de código pra passar. Feio é permitido nesta etapa.
3. Refatore com o teste verde.
4. Só então passe pro próximo comportamento.

Se te pedirem "implementa o handler X", você entrega **o teste primeiro**,
roda, mostra falhando, e só aí implementa. Se o pedido for explicitamente
"pula o teste", confirme antes: isso viola o invariante 5 do projeto.

## Que teste pra que camada

| Camada | Tipo | Ferramenta | Mocka o quê |
|---|---|---|---|
| `internal/sim` | table-driven + property + golden | stdlib + `pgregory.net/rapid` | nada, é puro |
| `internal/service` | unitário com fake em memória | stdlib | store, escrito à mão (não gere mock) |
| `internal/repo` | integração real | `testcontainers-go` + Postgres | nada, banco de verdade |
| `internal/http` | `httptest.Server` | stdlib | service, via interface pequena |
| firmware | golden vectors do core | `tinygo test` | hardware, via interface `Display` |

## Regras específicas

**Nunca mocke o que você não possui.** Não mocke `pgx`. Suba Postgres real
com testcontainers. Container sobe em ~2s e o teste vira confiável.

**Fake > mock gerado.** Um `type fakeStore struct{ events []sim.Event }` de 20 linhas
é melhor que gomock. Sem codegen neste projeto.

**Determinismo é testável.** Todo teste do `sim` roda duas vezes com a mesma entrada
e compara. Se der resultado diferente, o core está quebrado.

```go
func TestFoldIsDeterministic(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        evs := genEvents().Draw(t, "events")
        a := sim.Fold(sim.Genesis(), evs, fixedTime)
        b := sim.Fold(sim.Genesis(), shuffled(evs), fixedTime)
        if a != b {
            t.Fatalf("fold não é comutativo: %+v != %+v", a, b)
        }
    })
}
```

**Golden vectors são contrato entre runtimes.** `testdata/vectors/*.json` guarda
pares entrada/saída. Go, WASM e firmware rodam os mesmos vetores. Se divergirem,
o build quebra. Isso é o que garante que device e servidor concordam.

## Portão de qualidade

```makefile
test:
	go test ./... -race -count=1

cover:
	go test ./internal/sim/... -coverprofile=c.out
	@go tool cover -func=c.out | grep total | awk '{if ($$3+0 < 90) {print "cobertura do sim abaixo de 90%"; exit 1}}'

lint:
	golangci-lint run

check: lint test cover
```

Cobertura mínima de 90% **só no `internal/sim`**. Perseguir cobertura no resto
gera teste inútil. O core é onde vive a lógica, é lá que a métrica importa.

## O que não é teste

- Teste que só verifica que o mock foi chamado.
- Teste que reimplementa a lógica pra comparar com ela mesma.
- Teste com `time.Sleep`.
- Teste que depende de ordem de execução de outro teste.

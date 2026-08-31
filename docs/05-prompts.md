# Prompts prontos

Cole um por vez. Nunca peça mais de uma fatia vertical por prompt.

## Fase 1.1 — repositório de eventos

```
Leia as skills fera-context, data-layer e tdd-guard.

Implemente internal/repo/events.go: append em lote via unnest com
ON CONFLICT DO NOTHING, e leitura por cursor (pet_id, seq > $2 LIMIT $3).

Ordem obrigatória: primeiro o teste de integração com testcontainers-go
+ Postgres, rodando e FALHANDO. Me mostre a saída da falha. Só depois
implemente.

O teste precisa provar: (a) lote de 20 eventos insere 20, (b) reenviar o
mesmo lote insere 0 e não erra, (c) leitura por cursor devolve na ordem
de seq.
```

## Fase 1.2 — service de pet

```
Leia fera-context, go-backend, resilience-cache e tdd-guard.

Implemente internal/service/pet.go com Ingest e Get.

Get: tenta cache (otter v2), depois snapshot, faz sim.Fold dos eventos
desde folded_seq até agora, salva snapshot de volta se avançou.
Use singleflight na chave do pet.

As interfaces eventStore e snapshotStore ficam NESTE pacote, com no
máximo 4 métodos cada. Fakes em memória escritos à mão, sem codegen.

Teste primeiro. Cubra: cache hit, cache miss com snapshot, snapshot
ausente com replay do genesis, e stampede (100 goroutines concorrentes
resultando em 1 fold só).
```

## Fase 1.3 — HTTP

```
Leia fera-context, go-backend, api-contract e tdd-guard.

Implemente POST /v1/pets/{id}/events e GET /v1/pets/{id} usando chi.

Resposta do ingest no formato do api-contract, com accepted/duplicates/
rejected/cursor. Duplicata é 200, nunca 409.

Teste com httptest.Server e arquivos em internal/http/testdata/.
Primeiro os arquivos e o teste, depois o handler.
```

> **Feito até aqui:** fases 0, 1 e 3 completas; a 2 roda no Mac.
> Ver `docs/07-estado-do-projeto.md` pro retrato atual e pra ordem sugerida
> daqui pra frente. Os prompts abaixo ficam como registro do caminho.

## Fase 2.1 — firmware, primeiro pixel

```
Leia fera-context, firmware e tdd-guard.

Crie a interface Display e um fakeDisplay que escreve num buffer em
memória. Crie o renderer que desenha os 4 atributos e o sprite do estágio
num 128x64 mono.

Teste no Mac, sem placa: renderize um State conhecido e compare com o
buffer dourado em testdata/. Só depois plugue o driver ssd1306 real.
```

## Fase 2.2 — golden vectors cross-runtime (feito na fase 0)

```
Leia sim-core, firmware e tdd-guard.

Crie o gerador de vetores dourados: um cmd/gen-vectors que serializa
pares (state inicial, eventos, now) -> state final em JSON, cobrindo
todos os Kind e os casos de borda (clamp, cooldown, abandono longo).

Crie o teste que roda esses vetores no Go normal, e o mesmo teste rodando
sob `tinygo test`. Se divergirem, o teste falha.
```

## Quando algo der errado

```
Leia fera-context e tdd-guard.

Bug: <descrição>. Antes de propor correção, escreva um teste que reproduz
o bug e falha. Me mostre a falha. Só então corrija.
```

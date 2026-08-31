---
name: sim-core
description: Regras do núcleo de simulação da FERA (internal/sim). Carregue ao mexer em qualquer coisa dentro de internal/sim, ao adicionar tipo de evento, ao mudar regra de decaimento, evolução, temperamento ou encontro. Define o contrato de pureza e determinismo.
---

# internal/sim

O coração. Tudo que define o que a FERA **é** mora aqui, e nada mais mora aqui.

## Contrato absoluto

```go
func Fold(s State, evs []Event, t Tuning) State   // persistível
func Project(s State, now time.Time, t Tuning) View
```

- Puro. Sem I/O, sem clock global, sem goroutine, sem alocação não determinística.
- Comutativo por `event.ID`: a função ordena internamente antes de aplicar.
- Idempotente: aplicar o mesmo `event.ID` duas vezes é no-op.
- Compilável com TinyGo. Sem `reflect`, sem `encoding/json` no caminho quente,
  sem `fmt` fora de teste. Por isso a ordenação é insertion sort escrito à mão:
  `sort.SliceStable` puxa `reflect.Swapper` e infla o binário no device.

Imports permitidos: `time` (só o tipo, nunca `time.Now`). Hoje é o único.
Qualquer import além disso precisa de justificativa explícita.

## Fold e Project são separados, e isso não é estilo

`Fold` para no último evento. `Project` aplica o decaimento do tempo parado até
`now` e devolve `View`, um tipo **diferente** de `State`.

O tipo é diferente de propósito: torna impossível persistir a projeção. Se a
projeção virasse snapshot, seu `LastAtUnix` seria "agora", e todo evento
chegando depois com `At` anterior a esse instante seria descartado sem erro e
sem log. Que é o caminho normal de um device offline empurrando lote atrasado.

Regra: **o que vai pro banco sai de `Fold`. O que vai pra tela sai de `Project`.**

## Modelo do bicho

Quatro atributos em `int32`, escala `0..10000` (centésimos, `Max == 10000`).
Inteiro, nunca float: aritmética inteira é bit a bit idêntica no x86 do servidor,
no ARM do celular e no Xtensa do ESP32. `Pct()` converte pra 0..100 só na UI.

- **Vigor** — sobe com esforço, cai com inatividade
- **Ânimo** — sobe com interação e variedade, cai com monotonia
- **Saúde** — cai com excesso sem descanso, sobe com sono
- **Vínculo** — sobe com consistência e encontros BLE, cai devagar com ausência

Não adicione um quinto atributo sem eliminar um. Quatro é o limite do que
cabe numa tela de 128x64 e do que um humano consegue equilibrar.

## Tipos de evento

| Kind | Origem | Efeito principal |
|---|---|---|
| `effort` | wearable, app, IMU | +Vigor, -Saúde se sem descanso |
| `sleep` | wearable, app | +Saúde, +Ânimo |
| `interact` | botão do device | +Ânimo, +Vínculo pequeno |
| `encounter` | BLE | +Vínculo, chance de mutação de traço |
| `neglect_tick` | gerado pelo fold, não persistido | decaimento |

**Só `effort` e `encounter` fazem o bicho crescer.** Sono e botão mexem nos
atributos e não no `Growth`: o README abre com "só cresce com esforço físico
real" e "não dá pra alimentar apertando botão". Antes da calibragem um
sedentário que registrava sono e apertava o botão chegava a "jovem" em 57 dias
sem treinar um único dia.

`neglect_tick` **não é evento armazenado**. É calculado dentro do fold a partir
do delta entre o último evento e `now`. Nunca persista tempo passando.

## Determinismo com aleatoriedade

Mutação de traço precisa de sorte. Use seed derivado, nunca `math/rand` global:

```go
// seed determinístico: mesmo evento, mesma mutação, em qualquer runtime
seed := fnv64(ev.ID + s.PetID)
r := newSplitMix64(seed)
```

Isso permite que device e servidor cheguem no mesmo traço mutado sem trocar mensagem.

## Regra de balanceamento (importante)

Toda constante de balanceamento fica numa struct `Tuning`, passada como parâmetro,
com um `DefaultTuning`. Nunca hardcode número mágico dentro da lógica.

Tudo em `int32`, nunca `float32`. Zero número mágico dentro de `apply` ou
`decay`: se você escreveu um literal na lógica, ele pertence ao `Tuning`.

Isso deixa o balanceamento testável e ajustável sem tocar na lógica.

**O balanceamento está calibrado**, não chutado. Cada número do `DefaultTuning`
existe porque uma persona de `internal/sim/balanceamento_test.go` se comporta
como deve com ele e deixa de se comportar sem ele. Mexer em qualquer valor
quebra um teste que diz, em português, o que o bicho deveria fazer.

Se for mudar: rode `go test ./internal/sim -run TestBalanceamentoAtual -v` pra
ver a fotografia, ajuste, e regenere o pin e os vetores com `make vectors`.

`DefaultTuning` está pinado em `internal/sim/testdata/default_tuning.json`.
Cada golden vector carrega o `Tuning` que usou, então mudar o padrão não
quebraria vetor nenhum: o pin é o que impede device e servidor rodarem
balanceamentos diferentes sem ninguém perceber.

## Versionamento

`State` tem `SchemaVer`. Toda mudança de regra que altere resultado de fold
incrementa a versão e **invalida todos os snapshots**. Isso é esperado e barato,
porque o log é a verdade. Escreva a migração como: `DELETE FROM pet_snapshots`.

## Ao adicionar um evento novo

1. Adiciona o caso em `internal/sim/vectors/vectors.go` (`Cases()`).
2. Escreve o teste que falha. `TestGoldenVectorsCobremTodosOsCasos` já falha
   sozinho enquanto o vetor não existir.
3. Adiciona o `Kind` e o case no `apply`.
4. `make check`.
5. `make vectors` pra gerar o arquivo, e confira o diff antes de aceitar.
6. `tinygo build ./internal/sim` pra garantir que ainda compila pro device.

Nunca regenere vetor pra "consertar" um teste vermelho. Ou a regra mudou de
propósito (aí sobe `SchemaVer` antes de regerar), ou você quebrou o core.

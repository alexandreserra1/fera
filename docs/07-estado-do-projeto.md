# Estado do projeto

**30 de agosto de 2026.**

Este doc é o retrato do que existe, do que foi medido e do que ficou em aberto.
Os outros docs dizem como o sistema **deve** ser; este diz como ele **está**.

## Onde estamos

| Fase | O que é | Estado |
|---|---|---|
| 0 | `internal/sim` fechado, property tests, golden vectors | **feito** |
| 1 | API Go: ingest, snapshot, pull por cursor, auth | **feito** |
| 2 | Firmware que roda o sim offline e desenha na tela | **roda no Mac**, falta driver e placa |
| 3 | Sync device ↔ API | **feito**: push assinado sobre transporte próprio |
| 4 | BLE peer-to-peer | não começou, e há risco de plataforma |
| 5 | App Expo + Health/Strava | a página web já roda o sim em WASM |

O que roda hoje, de verdade:

```bash
make up                   # API + Postgres + migrações, num comando
make web                  # a FERA no navegador (WASM), em localhost:8000
make bicho                # a FERA no terminal, offline
make bicho-api            # a mesma FERA sincronizando contra a API
make check-all            # lint + testes + cobertura + TinyGo + link no Xtensa
```

## Números

| | |
|---|---|
| Código de produção | 5626 linhas |
| Código de teste | 5967 linhas |
| Funções de teste | 209, em 13 pacotes |
| Migrações SQL | 3 |
| Golden vectors do `sim` | 15 |
| Telas douradas do device | 6 |
| Casos de contrato HTTP | 19 |
| Cobertura do `internal/sim` | **100%** (portão exige 90%) |
| Dependências diretas | 9 |

As nove dependências: `chi`, `pgx`, `otter/v2`, `x/sync`, `x/crypto`, `uuid`,
`testcontainers` (teste), `rapid` (teste), `postgres` (teste). Nenhum ORM,
nenhum framework de DI, nenhum gerador de mock.

## O que existe

```
internal/sim/             o núcleo. Puro, sem dependência, roda nos três alvos
internal/sim/vectors/     catálogo dos golden vectors
internal/repo/            pgx, SQL na mão, testado contra Postgres real
internal/service/         casos de uso, interfaces no consumidor, fakes à mão
internal/http/            handlers, DTOs, router, auth (pacote httpapi)
internal/sig/             assinatura de requisição. Device e servidor importam
internal/device/display/  framebuffer, sprite, driver fake
internal/device/ui/       render(framebuffer, View). Puro
internal/device/store/    flash NOR: estado em duplo buffer + fila append-only
internal/device/hal/      fronteira com o hardware. Único que vai tocar "machine"
internal/device/input/    debounce de botão
internal/device/ulid/     identidade de evento gerada no device
internal/device/loop/     máquina de estados de energia
internal/device/net/      sync HTTP: JSON e HTTP/1.1 escritos à mão, sem net/http
cmd/api/                  o backend
cmd/feradev/              a FERA no terminal do Mac (bancada de calibragem)
cmd/firmware/             wiring do device
cmd/gen-vectors/          gera os golden vectors
cmd/gen-frames/           gera as telas douradas
migrations/               3 migrações numeradas
```

## Os invariantes, e o que os obriga

O `fera-context` lista cinco invariantes. Hoje cada um tem alguma coisa que
falha quando ele é violado, em vez de depender de disciplina:

1. **`sim` é puro.** Importa só `time`. `make device` roda os golden vectors
   sob TinyGo e linka o programa inteiro no Xtensa.
2. **Estado é derivado.** Não existe `UPDATE` de atributo em lugar nenhum.
   Os golden vectors travam o resultado do fold.
3. **Idempotência por ULID.** `PRIMARY KEY (pet_id, event_id)` com
   `ON CONFLICT DO NOTHING`, testado com 8 goroutines concorrentes.
4. **Sem JOIN quente.** Nenhuma query do `repo` tem `JOIN`.
5. **Teste antes do código.** Cobertura de 90% no `sim` é portão do `make`.

## Medições no alvo (`xiao-esp32s3`)

O que dá pra afirmar com `tinygo build -size`, não estimar:

| conteúdo | flash | RAM |
|---|---|---|
| `main` vazio (runtime TinyGo) | 5,9 KB | 10,9 KB |
| `internal/sim` | 9,1 KB | 11,3 KB |
| + `display` + `ui` | 12,1 KB | 15,4 KB |
| + `store` | 16,2 KB | 15,8 KB |
| firmware completo, sem rede | 44 KB | 20 KB |
| firmware + `net/http` (descartado) | 415 KB | 189 KB |
| **firmware completo, com rede** | **135 KB** | **57 KB** |

Vida útil da flash, simulando um ano de uso:

| padrão de escrita | apagamentos/setor/ano | vida |
|---|---|---|
| realista (7 saves/dia) | 1278 | 78 anos |
| a cada tick de 5 min | 52560 | 1,9 anos |
| fila de pendentes | 8 | 12500 anos |

Um dia realista no loop: 288 acordadas, 7 gravações de estado, 25 redesenhos.

## Decisões que contrariam os docs originais

Cada uma tem o motivo no código e no doc correspondente.

| Decisão | Contraria | Motivo |
|---|---|---|
| `Fold` e `Project` separados, `Project` devolve `View` | `docs/01` | `Fold` que decaía até "agora" descartava em silêncio todo evento atrasado |
| Tempo no `State` em `int64` unix | — | `time.Time` não sobrevive a round-trip por JSONB nem a `==` |
| `events` sem particionamento | `docs/02` | o DDL do doc não roda, e o conserto óbvio quebra a idempotência |
| SHA-256 no token, não argon2id | `api-contract` | hash salgado não é pesquisável; o token tem 256 bits de entropia |
| Golden de tela em ASCII, não PNG | `docs/06` | diff binário de render errado não é revisável |
| Loop em `internal/device/loop`, não em `main` | `docs/06` | lógica em `main` não tem como ser testada |
| Modo ativo desligado por padrão | `docs/06` | 750 acordadas/dia a mais para zero redesenho, sem animação |
| Assinatura BLAKE2s em vez de Bearer no device | `api-contract` | TLS não cabe na RAM; ver `docs/06` |
| HTTP/1.1 escrito à mão em vez de `net/http` | — | `net/http` linka `crypto/tls` sempre: 415 KB contra 135 KB |

## O que está em aberto

**Sigilo.** A assinatura dá autenticidade e integridade, não confidencialidade.
Quem estiver no caminho lê o treino. Quando pesar, a saída medida é BearSSL
via CGo (~7 KB de RAM), não o `crypto/tls` do Go.

**MAC simétrico.** O servidor guarda uma chave que também assina, então um dump
de backup permite forjar requisição. A saída é Ed25519 com a coluna `pubkey`
que a tabela `devices` já tem, ao custo de ~38 KB de RAM por causa do
`crypto/internal/fips140`.

**BLE não existe no ESP32-S3 no TinyGo.** O `espradio` diz "Bluetooth is now
supported on the esp32c3. Other processors coming soon." A fase 4 é o
diferencial declarado do projeto no README, e depende disso. Risco de
plataforma, não de código.

**Pull não implementado no device.** O contrato tem `GET /v1/pets/{id}/events`
e o servidor o serve, mas o device só faz push. Ele é autoridade local e não
precisa puxar os próprios eventos; o caso real é recuperar um device zerado.

**Driver de tela e medição de corrente.** Precisam de placa. As interfaces
esperam: `display.Buffered` só precisa de um `Show()`.

## Balanceamento: calibrado, não chutado

Era o último "TODO: calibrar" do projeto. A calibragem virou bancada de teste
em `internal/sim/balanceamento_test.go`: seis personas vivem 90 a 400 dias
simulados, e cada frase do README virou asserção.

O que a bancada revelou no balanceamento antigo não era "precisa de ajuste":

- **Vigor era 0 em todas as personas, inclusive no atleta.** O decaimento
  comia 28,8 pontos/dia e um treino de 700 kcal dava 14. O atributo não media
  nada.
- **Vínculo era 0 em todas.** Interagir dava 1 ponto, o decaimento levava 3,6.
- **Saúde era 100 em quase todas.** Nem o overtraining (zona 5, seis dias por
  semana, dormindo 5h) conseguia baixá-la.
- **O sedentário evoluía igual ao atleta.** Chegava a "jovem" no dia 57 sem
  treinar um dia, porque dormir dava 5 de growth e apertar o botão dava 2.

O último contraria a primeira linha do README: *"só cresce com esforço físico
real"* e *"não dá pra alimentar apertando botão"*. Isso não era gosto, era
violação do que estava escrito. Growth passou a vir só de esforço e encontro.

### O resultado, aos 90 dias

| persona | estágio | VIG | ANI | SAU | VIN |
|---|---|---|---|---|---|
| sedentário (só dorme e interage) | **ovo** | 0 | 100 | 100 | 46 |
| iniciante (2x/semana) | jovem | 86 | 100 | 100 | 46 |
| constante (3x/semana) | jovem | 96 | 100 | 100 | 46 |
| atleta (6x/semana, dorme bem) | adulto | 96 | 100 | 100 | 46 |
| overtraining (6x no talo, dorme mal) | adulto | 96 | **5** | **12** | 0 |
| sumiu (treina 2 semanas e para) | filhote | 0 | 0 | 0 | 0 |

As duas linhas que mostram que o sistema virou jogo:

O **sedentário** nunca sai de ovo, mas tem ânimo 100 e vínculo crescendo:
bicho feliz, fraco e que não evolui. O **overtraining** tem o mesmo estágio do
atleta e saúde 12 com ânimo 5: bicho forte, doente e arisco, que é exatamente
o *"se você treina demais e não dorme, ela fica arisca"* do README.

### Ritmo

Alvo pro dono constante (3x/semana, ~13 esforços/mês), com os limiares
40 / 100 / 400 / 1500:

| estágio | alvo | medido |
|---|---|---|
| filhote | ~1 semana | dia 8 |
| jovem | ~3 semanas | dia 22 |
| adulto | ~3 meses | dia 93 |
| veterano | ~1 ano | dia ~350 |

`SchemaVer` foi pra **3**, os golden vectors e o pin do `DefaultTuning` foram
regenerados. Os dois mecanismos fizeram exatamente o trabalho deles: falharam
e obrigaram a reconhecer a mudança em vez de deixá-la passar em silêncio.

## Auditoria contra os docs (31/08/2026)

Varredura de tudo que os `.md` e as skills prescrevem, conferido contra o
código e não de memória.

**Cumprido e verificado:** os cinco invariantes; nenhuma interface com mais de
4 métodos; nenhum `panic` fora de `main`; nenhum pacote `utils`/`helpers`/
`models`/`pkg`; `unnest` no append; nunca `UPDATE` em `events`; o `WHERE
folded_seq` no upsert; pool com lifetime; otter e singleflight; backoff com
jitter; formato de erro único; validação na borda sem lib de tag; chave de
contexto tipada.

**Corrigido nesta auditoria:**

- `GET /v1/pets/{id}/timeline`, o último endpoint do `api-contract` que
  faltava. É o mesmo log por cursor, em texto legível: não é projeção nova,
  porque histórico de UM pet é filtro por chave e não join.
- Rate limit por device (`x/time/rate` + otter com TTL), como o
  `resilience-cache` prescreve. Por device e não global: global faria um
  device com retry maluco derrubar o bicho de todo mundo.
- `Dockerfile` + `docker-compose.yml`: imagem distroless de 22 MB, migrações
  num serviço próprio que termina antes da API subir (o `data-layer` proíbe
  migração automática no boot). Portas configuráveis, porque porta fixa quebra
  na máquina de quem já usa a 8080.
- Dois `.md` órfãos removidos da raiz: uma cópia velha do `06` com as
  afirmações que já foram corrigidas no `docs/`, e uma duplicata do skill de
  firmware. Doc velho contradizendo o novo é pior que doc faltando.

**Desvios conscientes, não esquecimentos:**

| não feito | por quê |
|---|---|
| `internal/platform` | seria pacote de cerimônia: o cache mora no service, o clock é função injetada |
| `tern` ou `goose` | ferramenta de migração pra três arquivos SQL aplicados num laço |
| circuit breaker | o `resilience-cache` o reserva pra chamada a terceiro, e não há nenhuma |
| particionamento de `events` | o `docs/02` já registrou: só quando houver volume que justifique |
| `POST /v1/pets` | o `register` já cria o pet, por decisão de segurança |
| OpenTelemetry | só vale quando houver deploy; o `docs/01` põe o Caddy na frente |

## Bugs que os testes pegaram, e o que cada um ensinou

Vale registrar porque o padrão se repete.

| Bug | Como apareceu |
|---|---|
| Teste de dentes dando falso negativo 4x | grep por `--- FAIL` perdia falha de COMPILAÇÃO; passei a checar o código de saída |
| Sedentário evoluía igual ao atleta, contra o README | bancada de personas de balanceamento |
| `Fold` descartava evento atrasado em silêncio | teste escrito pra reproduzir suspeita de leitura |
| Registro da fila atravessando fronteira de setor | teste de **vida útil da flash**, escrito pra outra coisa |
| Reenvio de lote forçando replay do log inteiro | rodando o sistema de verdade, não em teste |
| Snapshot de schema velho travando a chave pra sempre | teste do `WHERE` do upsert |
| Overflow de `int32` no decaimento virando ganho | revisão do código, não teste |
| `TestStampede` era flaky e foi reportado como verde | rodando 30 vezes o que eu tinha rodado 1 |

Três vezes um "teste de dentes" (reverter a correção e conferir que o teste
acusa) revelou **falso negativo**: a mutação não tinha casado com o arquivo, ou
o helper de corrupção não corrompia nada. Verificar que a mutação de fato
mudou o arquivo virou parte do procedimento.

## Como continuar

O `docs/05-prompts.md` tem os prompts prontos. A ordem que faz sentido agora:

1. Comprar a placa e fazer o driver de tela e o `hal` de verdade.
2. Ligar o espradio (WiFi) e medir o consumo com multímetro.
3. Reavaliar BLE quando o `espradio` suportar o S3.
4. Fase 5: o app Expo, que não depende de hardware nenhum.

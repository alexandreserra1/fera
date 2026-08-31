# FERA

Codinome do projeto. Um bicho digital que **só cresce com esforço físico real**.
Não é Tamagotchi. Não dá pra alimentar apertando botão.

## A ideia em uma frase

Você tem uma criatura que vive num dispositivo físico de bolso (ESP32-S3 com tela).
Ela come treino: sessões registradas, tempo em zona de FC, passos, sono.
Se você não treina, ela definha. Se você treina demais e não dorme, ela fica arisca.
O temperamento dela muta com o **padrão** do seu treino, não com o volume.

Duas FERAs no mesmo ambiente se detectam por BLE e interagem sozinhas (troca de traço,
sparring, vínculo). Sem servidor no meio. O servidor só existe pra backup e histórico.

## Por que isso não é mais um clone de Tamagotchi

O nicho de "tamagotchi em ESP32" está saturado (dezenas de repos ativos).
O que quase nenhum tem:

1. **Input não falsificável.** Entrada vem de esforço real, não de tap.
2. **Social físico.** BLE peer-to-peer, criaturas se encontram porque os donos se encontram.
3. **Protocolo aberto e determinístico.** Qualquer um pode implementar um cliente
   e chegar exatamente no mesmo estado. Isso é o que faz virar plataforma e não brinquedo.
4. **Offline-first de verdade.** O device é autoridade local, o servidor reconcilia.

Comunidade que compra essa briga: maker/ESP32, open hardware, fitness tech, Go embarcado.
As três primeiras aceitam bem qualquer coisa aberta e bem documentada. A quarta é onde
mora a diferenciação.

## Créditos de arte

Os bichos vêm do [1-Bit Pack do Kenney](https://kenney.nl/assets/1-bit-pack),
sob **CC0 1.0** (domínio público). CC0 não exige atribuição; o crédito está
aqui porque é justo.

A arte original é 16x16. O `cmd/import-sprite` amplia pra 64x64 com Scale2x
(EPX), que preenche cantos internos a partir dos vizinhos — ampliar por
repetição transformaria cada pixel num bloco e o bicho viraria Minecraft.

Pra trocar um bicho: `make bichos-png`, edite no [Piskel](https://www.piskelapp.com),
e traga de volta com `go run ./cmd/import-sprite -in novo.png -w 64 -h 64 -suavizar -nome adulto`.

## O que tem neste repo

```
docs/                     decisões de arquitetura, contratos, BOM de hardware
docs/07-estado-do-projeto.md   o retrato atual: o que existe, o que foi medido
.claude/skills/           as Skills pro Claude seguir (leia SKILLS.md abaixo)
internal/sim/             núcleo da simulação, puro e testado (código real, roda hoje)
internal/repo/            pgx, SQL na mão. Testado contra Postgres real.
internal/service/         casos de uso + as interfaces que ele consome. Fakes à mão.
internal/http/            handlers, DTOs, router. Pacote httpapi (ver dto.go).
cmd/api/                  o binário. Wiring explícito, shutdown gracioso.
migrations/               SQL numerado. Nada de migração automática no boot.
internal/sim/vectors/     catálogo dos golden vectors (andaime de teste, fora do core)
internal/sim/testdata/    os vetores gerados: o contrato entre servidor, WASM e device
internal/device/display/  framebuffer, sprite, driver fake. Não importa "machine".
internal/device/ui/       render(framebuffer, View). Puro, testado no Mac.
internal/device/store/    estado e fila de pendentes em flash NOR. MemFlash pra teste.
internal/device/hal/      fronteira com o hardware. Único que vai importar "machine".
internal/device/input/    debounce de botão. Puro.
internal/device/ulid/     identidade de evento gerada no device.
internal/device/loop/     máquina de estados de energia. Onde mora o "quando".
internal/device/net/      sync HTTP. JSON escrito à mão, sem encoding/json.
internal/sig/             assinatura de requisição. Device e servidor importam.
cmd/feradev/              a FERA no terminal do Mac. Bancada de calibragem.
cmd/firmware/             wiring do device.
cmd/gen-vectors/          gera os vetores. Só grava com -write, nunca automático.
cmd/gen-frames/           gera as telas douradas do device.
cmd/simcheck/             prova que o core linka no Xtensa. Não é firmware.
```

```bash
make up          # API + Postgres + migrações, em um comando
make down        # derruba e apaga o volume

# se a 8080 já estiver ocupada:
FERA_PORT=8090 docker compose up --build
```

```bash
# registrar um device: devolve device_id, pet_id e o token (aparece uma vez só)
curl -sX POST localhost:8080/v1/devices/register

# daí em diante tudo exige o Bearer, e só no próprio pet
curl -s localhost:8080/v1/pets/$PET -H "Authorization: Bearer $TOKEN"
```

```bash
make check      # gofmt + vet + go test -race + cobertura do sim >= 90%
make check-all  # o acima + os mesmos vetores sob TinyGo + link no ESP32-S3
make vectors    # regenera os golden vectors (ato deliberado, veja sim-core)
make bicho      # roda a FERA no terminal, offline (a/i/m/b/q)
make bicho-api  # o mesmo, sincronizando contra uma API em localhost:8080
make tamanho    # quanto o programa do device ocupa no ESP32-S3
make web        # a FERA no navegador (WASM), em http://localhost:8000
make telas      # desenha as telas do device no terminal
make frames     # regenera as telas douradas
```

O `make device` é o que sustenta a premissa "um core, três alvos": os mesmos
golden vectors passam no Go do servidor e no TinyGo, e o programa INTEIRO do
device linka no alvo real (`xiao-esp32s3`, Xtensa 32-bit) em 44 KB de flash e
20 KB de RAM.

O `make bicho` roda esse mesmo programa no terminal do Mac, com relógio
acelerado. É a bancada pra calibrar o balanceamento, que hoje é todo chute.

## Como usar isso com o Claude

1. Sobe o repo (ou a pasta) num projeto do Claude / Claude Code.
2. As Skills em `.claude/skills/` são carregadas automaticamente pelo Claude Code.
3. Peça uma fatia vertical por vez. Nunca "constrói o sistema".
   Ex: "implementa o endpoint POST /v1/pets/{id}/events seguindo api-contract e tdd-guard".

## Ordem de construção sugerida

| Fase | Entrega | Tempo realista |
|---|---|---|
| 0 | `internal/sim` fechado, property tests e golden vectors | **feito** |
| 1 | API Go: ingest de eventos + snapshot + sync cursor | **feito** |
| 2 | Firmware que roda o sim offline e desenha na tela | **roda no Mac**; falta driver e placa |
| 3 | Sync device <-> API | **feito** (push assinado; falta trocar o transporte) |
| 4 | BLE peer-to-peer | 2 semanas |
| 5 | App (Expo) e integração Health/Strava | 2 semanas |

Fase 0 e 1 dão pra fazer 100% no MacBook sem hardware nenhum.
Não compre placa antes da fase 0 estar fechada.

Fase 0 fechou: `Fold` e `Project` separados, tempo em `int64` unix, property
tests com `rapid`, 15 golden vectors e o core linkando no ESP32-S3.

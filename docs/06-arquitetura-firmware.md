# Arquitetura do firmware

Este doc substitui o tick de 200ms que estava na primeira versão do kit.
Aquele número assumia display volátil. Com memory LCD ele está errado.

## Escolha de tela e por que ela decide a arquitetura

O framebuffer é o maior bloco de RAM do firmware. A resolução escolhida
determina se você precisa de PSRAM, e PSRAM determina se desenhar vira
um problema de DMA.

| Tela | Framebuffer | Consequência |
|---|---|---|
| SSD1306 128x64 mono | 1 KB | trivial |
| Sharp LS013B7DH03 168x144 mono | 3 KB | trivial, e mantém imagem sem energia |
| ST7789 240x240 RGB565 | 112 KB | cabe na SRAM interna, apertado |
| AMOLED 466x466 RGB565 | 434 KB | exige PSRAM, banda vira gargalo |

**Escolhida: Sharp memory LCD 168x144.** É a tela do Pebble.
Reflexiva, legível em sol direto, sem backlight, e retém a imagem
consumindo microampères. Isso permite o bicho ficar sempre visível
com o MCU dormindo, que é a diferença entre um brinquedo e um objeto.

Alternativa barata pra prototipar: SSD1306. Mesmo código, driver diferente.

## Orçamento de RAM (ESP32-S3, 512 KB SRAM interna)

```
framebuffer duplo        6 KB    2 x 3024 bytes
fila de pendentes        4 KB    64 eventos x ~64 B
sim.State              < 1 KB    a struct inteira
sprites                  0 KB    ficam na flash mapeada, sem cópia
stack WiFi (só ligado)  50 KB    maior consumidor, por isso fica off
heap TinyGo             32 KB    teto conservador
stacks + misc           20 KB
------------------------------
total                 ~112 KB    sobram ~400 KB
```

O `sim` e a UI não precisam de PSRAM.

**Mas a afirmação original deste doc, "PSRAM não é necessária, isso derruba
custo da placa", estava errada.** A placa escolhida no `docs/04` é o
ESP32-S3R8, e as três variantes do XIAO ESP32-S3 (normal, Sense e Plus) trazem
8 MB de PSRAM on-chip. Não existe versão sem, então não há custo a derrubar.
O que a frugalidade de RAM compra aqui é outra coisa, e é real: corrente de
repouso menor e folga pro stack de WiFi, que é o maior consumidor de todos.

Medido, não estimado. `make device` linka `cmd/simcheck` pro alvo
`xiao-esp32s3` e o `-size` dá:

| conteúdo | flash | RAM |
|---|---|---|
| só `internal/sim` | 9151 B | 11304 B |
| `sim` + `display` + `ui` | 12073 B | 15392 B |

Ou seja, a camada de tela inteira (framebuffer de 1 KB, fonte 3x5 completa e
os cinco bichos) custou **+2,9 KB de flash e +4,1 KB de RAM**. Sobram mais de
380 KB. O `sim` e a UI não são o problema de orçamento aqui; a stack de WiFi é.

## Máquina de estados de energia

```
        botão / timer 5min
  SLEEP ──────────────────► TICK ──► (mudou algo visível?)
    ▲                        │              │ não
    │                        │ sim          └──► SLEEP
    │                        ▼
    │                     RENDER
    │                        │
    │                   (foi botão?)
    │            não ◄───────┴───────► sim
    │             │                     │
    └─────────────┘                     ▼
                                    ACTIVE 15s @ 10fps
                                        │
                                        └──► SLEEP

  1 a 2x por dia: SLEEP ──► SYNC (wifi on ~10s) ──► SLEEP
```

Consumo médio estimado: ~0,7 mA. Com 500 mAh dá semanas no papel.
Divida por 3 pro mundo real: 10 a 15 dias. Meça, não confie na conta.

## Camadas

```
cmd/firmware/main.go      wiring do device. Só isso.
cmd/feradev/main.go       a FERA no terminal do Mac (bancada de calibragem)
internal/device/loop/     a máquina de estados de energia
internal/device/hal/      ÚNICO pacote que importa "machine"
internal/device/display/  Buffer, Sprite, Display, fake
internal/device/ui/       render(framebuffer, View). Puro.
internal/device/input/    debounce, fila de botões
internal/device/ulid/     identidade de evento gerada no device
internal/device/store/    flash: estado em duplo buffer + ring buffer de pendentes
internal/device/net/      sync HTTP, liga wifi só na janela  (a fazer)
internal/device/ble/      encontros peer-to-peer            (a fazer)
internal/sim/             MESMO pacote do servidor, importado
```

**O loop mora em `internal/device/loop`, não em `main`** (correção da v1 deste
doc). As regras difíceis do device são "quando gravar" e "quando desenhar", e
lógica dentro de `main` não tem como ser testada. Os dois binários só fazem
wiring, e a máquina de estados tem teste pra cada regra.

**Regra de ouro: só `hal` importa `machine`.** Consequência: `ui`, `store`,
`net` e `sim` rodam com `go test` no Mac. Você testa o renderer comparando
o framebuffer com uma tela dourada, sem placa ligada.

O `ui.Render` tem a assinatura `(*display.Buffer, sim.View)`: framebuffer
entra, pixels saem. Sem interface, sem driver, sem relógio. É essa assinatura
que faz o teste de tela ser um `go test`.

Ele recebe `sim.View` e não `sim.State` porque a tela mostra o bicho AGORA,
com o decaimento do tempo parado já aplicado. O loop faz `Project` e passa o
resultado. Ver a separação Fold/Project no `docs/01`.

### Golden em ASCII, não em PNG (correção da v1 deste doc)

Este doc pedia PNG dourado. Um render errado em PNG produz um diff binário que
ninguém consegue revisar: você vê que mudou, não O QUE mudou. Os goldens são
`.txt` com `.` e `#`, um caractere por pixel, e o diff mostra o desenho.
`make telas` desenha tudo no terminal; `make frames` regrava os arquivos.

Se você se pegar copiando código do `sim` pro firmware, parou. Importe.

## Regras de alocação

TinyGo tem GC conservativo. Se o loop em regime não aloca, o GC nunca roda
e você não tem pausa nem fragmentação. Para isso:

- Todos os buffers pré-alocados no init. Zero `make` dentro do loop.
- Sem `fmt`. Ele puxa reflection e explode o binário. Escreva um `itoa`
  de 15 linhas pra formatar número na tela.
- Sem concatenação de string no loop. Use `[]byte` reaproveitado.
- Sprites como arrays const ou `//go:embed`, lidos direto da flash.
- Sem `encoding/json` no device. Serialize o evento à mão em binário
  compacto, e converta pra JSON só no envio (ou mande binário e converta
  no servidor).

## Desgaste de flash

NVS tem ordem de 100 mil ciclos de escrita por setor. Se você salvar a
cada tick de 5 min, são 105 mil escritas por ano e o device morre.

Salve só em: mudança de estágio, botão pressionado, antes de deep sleep
por causa de bateria baixa, e depois de sync bem sucedido.
A fila de pendentes é ring buffer com escrita append, não rewrite.

Regra prática: se você não consegue justificar uma escrita, ela não acontece.

Isso deixou de ser prosa: `TestVidaDaFlashEmUsoRealista` simula um ano dos
dois jeitos e imprime a vida útil.

| padrão de escrita | apagamentos por setor ao ano | vida |
|---|---|---|
| realista (7 saves/dia) | 1278 | 78 anos |
| a cada tick de 5 min | 52560 | **1,9 anos** |
| fila de pendentes (5 eventos/dia) | 8 | 12500 anos |

## Layout da flash (`internal/device/store`)

```
setor 0 e 1   estado, gravado ALTERNANDO entre os dois
setor 2..N    fila de pendentes, ring buffer append-only
```

O estado usa dois setores porque gravar exige apagar antes. Se a energia cair
entre o `Erase` e o `Write`, o setor fica vazio; alternando, a queda destrói o
registro novo e o anterior continua íntegro. Um setor só seria uma janela em
que o bicho some.

Três regras que não são estilo, são consequência da NOR:

1. **Registro nunca atravessa fronteira de setor.** 4096/80 dá 51,2, então
   tratar a fila como região contígua faria o slot 51 começar no byte 4080 e
   cruzar pro setor seguinte. Apagar um setor destruiria metade de um registro
   do vizinho. Os 16 bytes que sobram por setor são o preço.
2. **Status só transiciona zerando bit**: `0xFF` livre, `0xFE` gravado, `0xFC`
   sincronizado. É isso que permite marcar como enviado sem reescrever o
   registro e sem apagar setor.
3. **A cabeça da fila sai do maior `seq`, não do primeiro slot livre.** Depois
   que a fila dá a volta existem slots livres ANTES dos gravados, e a busca
   ingênua põe a cabeça no meio do histórico.

Fila cheia devolve `ErrFilaCheia`, nunca descarta em silêncio: evento perdido
é treino que o dono fez e o bicho não comeu.

### Custo medido no alvo

`tinygo build -size -target=xiao-esp32s3 ./cmd/simcheck`:

| conteúdo | flash | RAM |
|---|---|---|
| só `internal/sim` | 9151 B | 11304 B |
| + `display` + `ui` | 12073 B | 15392 B |
| + `store` | 16161 B | 15816 B |
| programa inteiro (`cmd/firmware`) | 44237 B | 20256 B |

Do binário completo (sem rede), o código do projeto é ~12 KB. O resto é stdlib: `time`
sozinho custa 10,7 KB e puxa `internal/reflectlite` (4,7 KB) junto. Dava pra
tirar trocando `time.Time` por int64 em todo o device, mas são 15 KB numa
flash de 8 MB, e o `sim` já usa `time.Time` na borda do Event. Não vale.
Nenhum `fmt` foi linkado, que é o que realmente importava checar.

## O elefante: HTTPS no device custa 10x o resto do firmware

Medido linkando `internal/device/net` no `cmd/firmware`:

| | flash | RAM |
|---|---|---|
| device sem rede | 44 KB | 20 KB |
| com `net/http` + TLS | **416 KB** | **188 KB** |

O `net/http` sozinho é 15 KB. O peso é TLS: `math/big` (RSA), `encoding/asn1`
(X.509), a suíte FIPS de crypto (sha3, aes, sha512) e `fmt` (10 KB, puxado
pelo x509 e não por código nosso).

Cabe nos 512 KB de SRAM do ESP32-S3 — 188 KB mais ~50 KB do stack de WiFi
deixam ~270 KB livres — mas estoura em quase 2x o orçamento de ~112 KB que
este doc estimava. **A conta de papel estava errada por não contar TLS.**

### Medições que resolveram a questão

Todas no alvo `xiao-esp32s3`, com `tinygo build -size`:

| conteúdo | flash | RAM | notas |
|---|---|---|---|
| `main` vazio | 5,9 KB | 10,9 KB | custo do runtime TinyGo |
| firmware sem rede | 44 KB | 20 KB | sim + ui + display + store + loop |
| + `net/http` (com ou sem TLS explícito) | 415 KB | 189 KB | `net/http` linka `crypto/tls` **sempre** |
| X25519 + ChaCha20 em Go puro | +87 KB | +38 KB | 36 KB são `fips140` nunca chamado |
| **BLAKE2s com chave** | **+2,5 KB** | **+0,7 KB** | zero `fips140` |
| TCP cru + HTTP à mão + assinatura | 94 KB | 41 KB | **nenhum** `crypto/tls`, `x509` ou `math/big` |

Duas lições que só apareceram medindo:

**O módulo `crypto/internal/fips140` é a gordura escondida do Go embarcado.**
Basta tocar em `curve25519` ou `crypto/sha256` pra entrarem AES (17 KB),
SHA-3 (8,9 KB) e SHA-512 (5,9 KB), que nunca são chamados: os auto-testes do
módulo referenciam todos os algoritmos e derrotam a eliminação de código morto.
BLAKE2s escapa por não estar no módulo, e ainda tem modo com chave, o que
dispensa a construção HMAC.

**Assinar não remove o TLS do binário; trocar o transporte remove.** O
`net/http` linka `crypto/tls` incondicionalmente, então o firmware com
assinatura E `net/http` continuava em 189 KB. Um cliente HTTP/1.1 escrito à
mão sobre socket TCP (que o espradio oferece) derruba pra 41 KB.

Três saídas, e a escolha ainda não foi feita:

1. **Aceitar.** Cabe. O S3 tem 8 MB de flash e 512 KB de SRAM, e a janela de
   sync é de segundos, uma ou duas vezes por dia. Custo: quase nenhuma folga
   pra crescer, e corrente de pico maior enquanto o WiFi está ligado.
2. **mbedTLS do ESP-IDF** em vez do `crypto/tls` do Go. É acelerado por
   hardware no S3 e muito menor. Custo: sair do TinyGo puro, que é a premissa
   do `docs/00`.
3. **O celular como ponte.** Device fala BLE com o app, o app fala HTTPS com a
   API. Tira WiFi e TLS do device inteiro, e o projeto já tem BLE (fase 4) e
   app (fase 5) no roteiro. Custo: sync deixa de funcionar sem o celular por
   perto, o que contraria "o device é autoridade local e sincroniza sozinho".

### O que foi feito

A saída escolhida foi a quarta, que a medição revelou: **assinar a requisição
em vez de mandar o segredo**, e trocar o transporte pra sair do `net/http`.

`internal/sig` assina método, caminho (com query), instante e digest do corpo
com BLAKE2s com chave. O token nunca cruza o fio. Verificado ponta a ponta:
0 ocorrências do token no log do servidor depois de um sync completo.

O que isso dá: autenticidade e integridade. Adulterar o corpo em trânsito é
recusado (`TestCorpoAdulteradoEmTransitoEhRecusado`).
O que não dá: sigilo. Quem estiver no caminho lê o treino.

Bearer e assinatura convivem: o app usa Bearer sobre HTTPS, o device assina.

**Transporte trocado.** O `net/http` saiu do device. `internal/device/net`
fala HTTP/1.1 sobre `net.Dial` + `bufio`, escrito à mão, e o resultado medido:

| | flash | RAM |
|---|---|---|
| firmware com `net/http` | 415 KB | 189 KB |
| firmware com TCP cru | **135 KB** | **57 KB** |

−280 KB de flash e −132 KB de RAM, e nenhum `crypto/tls`, `crypto/x509`,
`math/big` ou `fips140` no binário.

### Por que à mão, e não uma biblioteca

`soypat/lneto` (sucessor do `soypat/seqs`) é sério e roda em poucos kB, mas
traz a pilha TCP/IP INTEIRA em userspace. O espradio já entrega sockets
Berkeley e `net.Dial` funcionando: trocar uma pilha que funciona por outra
pra ganhar a moldura de HTTP não paga. O que faltava era formatar requisição e
ler resposta, e isso são ~180 linhas sobre `net` e `bufio` da stdlib.

O `net/http` não tem modo "sem TLS": `crypto/tls` está no grafo de imports
dele, não numa opção de configuração.

**O mesmo cliente roda no host e no device**, de propósito. Um transporte no
teste e outro na placa seria o pior tipo de build tag: o caminho que roda no
hardware nunca seria exercitado. Os testes usam `httptest.Server`, que fala
HTTP de verdade.

O piso de ~57 KB é o próprio pacote `net` (que puxa `fmt` pelos tipos de erro
dele). Descer disso exigiria trocar `net` por `lneto`, e aí sim a conversa
seria outra.

### O relógio e a janela de assinatura

A janela é de 24h, generosa de propósito: o device não tem RTC confiável
depois de um reboot a frio, e uma janela apertada o deixaria sem conseguir
autenticar até acertar o relógio, que ele só acerta sincronizando.

Custa pouco porque a defesa de verdade contra replay não é o relógio:
reenviar um lote capturado insere os mesmos ULIDs, o `ON CONFLICT DO NOTHING`
os descarta, e o replay vira no-op.

A recusa carrega `X-Fera-Time` com o relógio do servidor, pra que um device
perdido no tempo tenha como se corrigir em vez de ficar preso.

### O que MAC simétrico custa

O servidor guarda uma chave que também **assina**. Um dump de backup permite
forjar requisição, coisa que o Bearer com hash não permitia.

A saída sem esse problema é Ed25519 com a coluna `pubkey` que a tabela
`devices` já tem desde o `docs/02`. Custa ~38 KB de RAM no device por causa do
`fips140`, contra 672 bytes de hoje. Fica pra quando pesar mais.

## O net/http do TinyGo não é o da stdlib

`http.Transport` no TinyGo não tem `MaxIdleConns`, `MaxIdleConnsPerHost`,
`IdleConnTimeout` nem `ForceAttemptHTTP2`. Configurar esses campos é erro de
COMPILAÇÃO no alvo, não surpresa em runtime — e foi assim que
`internal/device/net/cliente_tinygo.go` nasceu, separado por build tag do
`cliente_host.go`.

A lição vale além deste caso: conselho de tuning de servidor (o skill
`resilience-cache`, por exemplo) não atravessa pro device sem ser verificado
no alvo.

Cuidado ao medir: `MemFlash` é dublê de teste e guarda a flash inteira em RAM.
Linkado no binário do device ele sozinho inflou a medição pra 90 KB. No
hardware a flash é flash, e o `hal` fala com o controlador.

## Relógio

ESP32-S3 tem RTC que sobrevive ao deep sleep, mas não a reset por bateria zero.
O `sim` já tolera isso: o decaimento é calculado do delta entre eventos, e
o ULID carrega timestamp. Depois de um reset frio, o device marca o estado
como "relógio suspeito" e corrige no primeiro sync.

## Ordem de construção do firmware

1. ~~Blink~~ — o `tinygo build -target=xiao-esp32s3` já prova toolchain.
2. ~~`fakeDisplay` + renderer + tela dourada, no Mac.~~ **feito**
3. ~~Botões com debounce, gera evento, roda o `sim`.~~ **feito**
4. ~~Persistência em flash, sobrevive a reboot.~~ **feito**
5. ~~Loop e máquina de estados de energia.~~ **feito**, e roda no terminal
   com `make bicho`.
6. Driver Sharp real. **Precisa de placa.**
7. Deep sleep e medição de corrente com multímetro. **Precisa de placa.**
8. Sync HTTP.
9. BLE.

Os passos 2 a 5 saíram inteiros sem hardware, o que muda a ordem original: a
placa deixou de ser necessária pra tudo menos os passos que medem elétrica.

## A bancada de calibragem

```bash
make bicho                                  # relógio a 1800x
go run ./cmd/feradev -velocidade=86400      # um dia por segundo
```

Roda o MESMO `sim`, o MESMO `loop` e a MESMA semântica de flash NOR que vão
pro ESP32. O que muda é só o `hal` e o driver de tela. O estado vive num
arquivo (`fera.flash`), então fechar e reabrir continua de onde parou.

Isso existe por causa dos `// TODO: calibrar` do `sim.DefaultTuning`: não dá
pra calibrar balanceamento lendo constante. Primeira observação da bancada:
**três dias largado zeram vigor, ânimo e vínculo**, enquanto saúde quase não
se move. Se isso é rápido demais, o número a mexer é `DecaiVigorHora`.

Passos 1 a 3 dão uns 2 fins de semana. É normal.
Se o passo 3 levar um mês, o problema é o driver, não você.

## Sobre o case

Papelão na primeira semana, pra descobrir a ergonomia antes de modelar.
Depois Fusion 360 ou OnShape (roda no browser, funciona no Mac M1) e
impressão 3D. FabLab da UFSC e serviços em Floripa cobram barato.

A forma comunica o conceito. Não faça o oval do Tamagotchi. Faça algo que
prenda na mochila ou caiba no bolso do short, porque o bicho vai junto no treino.

---
name: firmware
description: Firmware da FERA no ESP32-S3. Carregue ao escrever código de device, driver de display, gerenciamento de energia, persistência em flash, BLE ou sync. Cobre TinyGo, o caminho alternativo em C, e os golden vectors.
---

# Firmware

Leia `docs/06-arquitetura-firmware.md` antes de escrever qualquer código de
device. Ele tem o orçamento de RAM, a máquina de estados de energia e as
regras de desgaste de flash.

## Alvo

Seeed XIAO ESP32-S3 com TinyGo v0.41+. WiFi nativo via `espradio`, flash via
`espflasher` sem ferramenta externa.

Tela: Sharp memory LCD 168x144 mono (LS013B7DH03). Framebuffer de 3 KB,
retém imagem sem energia, legível no sol. Sem PSRAM.
SSD1306 128x64 como alternativa barata de protótipo, mesmo código.

```bash
tinygo flash -target=xiao-esp32s3 ./cmd/firmware
```

Alvo alternativo documentado (se o gráfico apertar): ESP-IDF em C + LVGL.
Ver `docs/00-adr-linguagens.md`.

## Princípio: o device é autoridade local

Ele **não** consulta o servidor pra saber o estado do bicho. Ele roda
o mesmo `internal/sim` que o servidor. Sem rede, funciona igual, indefinidamente.
Sync é backup, não dependência.

## Estrutura

```
cmd/firmware/main.go       loop principal, wiring de pinos
internal/device/display/   interface Display + drivers
internal/device/input/     debounce de botão
internal/device/store/     persistência em NVS/flash
internal/device/net/       sync HTTP
internal/device/ble/       encontros peer-to-peer
internal/sim/              MESMO core do servidor. Não copie, importe.
```

Se você se pegar copiando código do `sim` pro firmware, pare. Isso quebra
o invariante 1 do projeto. Importe o pacote.

## Interface de display, testável sem hardware

```go
type Display interface {
    Clear()
    Blit(x, y int16, sprite Sprite)
    Show() error
}
```

Implementações: `ssd1306.Device`, e um `fakeDisplay` que grava num buffer
em memória. Todo teste de UI roda no Mac, sem placa, comparando o buffer
com um PNG dourado.

## Loop principal: dirigido por evento, não por tick

Com memory LCD a imagem fica na tela sem energia. Renderizar em loop é
desperdício puro. O loop é uma máquina de estados:

```go
for {
    reason := hal.Sleep(untilNextWake(state))  // botão, timer 5min, ou sync
    now := clock.Now()

    evs := input.Drain()
    next := sim.Fold(state, evs, now, tuning)

    if visiblyChanged(state, next) {
        ui.Render(next)             // desenha UM frame
    }
    if len(evs) > 0 {
        store.AppendPending(evs)
        ui.Animate(next, 15*time.Second, 10) // só quando o dono interage
    }
    if syncDue(now) {
        net.Sync(store.Pending())
    }
    state = next
}
```

Ocioso: acorda a cada 5 min. Ativo: 10 fps por 15 s. Nunca 200 ms em loop.

## Energia

- Deep sleep é o estado padrão, não a exceção.
- WiFi liga 1 a 2x por dia por ~10s. Ligado direto mata 500mAh em horas.
- Alvo: 10 a 15 dias reais. Meça com multímetro, não confie na planilha.
- A conta de papel dá semanas. Divida por 3 pro mundo real.

## Alocação zero no regime

TinyGo tem GC conservativo. Se o loop não aloca, o GC nunca roda.

- Buffers pré-alocados no init. Nenhum `make` dentro do loop.
- Proibido `fmt`: puxa reflection e explode o binário. Escreva `itoa` na mão.
- Proibido `encoding/json` no device. Serialize evento em binário compacto.
- Sprites como const ou `//go:embed`, lidos da flash, nunca copiados pra RAM.

## Desgaste de flash

NVS aguenta ~100k ciclos por setor. Salvar a cada tick mata o device em um ano.
Escreva só em: mudança de estágio, botão pressionado, bateria crítica, pós-sync.
A fila de pendentes é ring buffer append-only, nunca rewrite.

## Persistência

Estado e fila de eventos na NVS. Escreva com **wear leveling em mente**:
não salve a cada tick. Salve em: mudança de estágio, botão pressionado,
antes de deep sleep. Flash tem ~100k ciclos de escrita.

## Golden vectors

`testdata/vectors/*.json` são compartilhados entre servidor e firmware.
`tinygo test ./internal/sim` roda os mesmos vetores no alvo.
Divergência = build quebrado. Isso é o contrato entre os dois runtimes.

## Regra prática

Qualquer bug que der no device, primeiro tente reproduzir num teste em Go
rodando no Mac. Debugar com `println` na serial é o último recurso, não o primeiro.

# Hardware

## Filosofia: dois estágios

**Estágio 1 (protoboard, ~R$ 250):** funciona, é feio, você aprende tudo.
**Estágio 2 (PCB + case impresso, ~R$ 400 por unidade em lote de 5):** só depois
que o firmware estiver estável. Nunca projete PCB antes do software funcionar.

## BOM estágio 1

| Item | Modelo sugerido | Por quê |
|---|---|---|
| MCU | **Seeed XIAO ESP32-S3** | É *featured board* do TinyGo, WiFi nativo suportado desde a v0.41, minúsculo (21x17mm), USB-C. Se você for de TinyGo, é a escolha com menos atrito. Chip **ESP32-S3R8**: 512 KB de SRAM interna, **8 MB de PSRAM** e 8 MB de flash. |
| MCU alternativo | Waveshare ESP32-S3 1.75" AMOLED redondo | Tela linda, 466x466, touch. Só vale se você for pro caminho C/ESP-IDF+LVGL. |
| Display | SSD1306 OLED 128x64 I2C ou ST7789 240x240 SPI | Ambos têm driver em `tinygo.org/x/drivers`. Comece no SSD1306: mono, barato, e sprite mono força identidade visual boa. |
| Botões | 3x tactile 6mm + pull-up interno | Alimentar / Interagir / Menu |
| Bateria | LiPo 3.7V 500mAh + TP4056 | O XIAO já tem circuito de carga da bateria |
| Buzzer | passivo piezo | som é 40% da personalidade |
| IMU (opcional) | MPU6050 ou LSM6DS3 | detectar movimento do dono sem depender do celular |

Onde comprar no Brasil: Curto Circuito, Eletrogate, MakerHero. AliExpress se der pra
esperar 30 dias. Evite marketplace genérico pra ESP32, tem muito clone com PSRAM falsa.

**Sobre a PSRAM:** as três variantes do XIAO ESP32-S3 vêm com 8 MB dela, então
não é opcional nem encarece a escolha. O firmware não depende dela (o programa
inteiro medido cabe em 20 KB de SRAM sem rede), mas ela existe e é folga real
se um dia o framebuffer crescer ou o TLS entrar.

**Sobre BLE:** o `espradio` do TinyGo suporta Bluetooth **só no ESP32-C3** por
enquanto. No S3 ainda não. A fase 4 depende disso; ver `docs/07`.

## Sobre "fazer na mão"

O case é onde o projeto vira **seu**. Opções, em ordem de custo:

1. **Papelão + cola** para validar ergonomia. Sério. Faça isso na primeira semana.
2. **Impressão 3D.** Florianópolis tem serviço barato, e tem FabLab na UFSC.
   Modele no Fusion 360 (grátis pra hobby) ou OnShape (roda no browser, funciona no Mac).
3. **Corte a laser em acrílico** se quiser algo mais "objeto" e menos "protótipo".

Sugestão de identidade: não faça oval de Tamagotchi. Faça algo que caiba no bolso
do short de treino ou clipe na mochila. A forma comunica que o bicho vai junto no treino.

## Ferramental no MacBook Pro 2021 (M1)

Tudo roda nativo em ARM:

```bash
brew install go tinygo-org/tools/tinygo
brew install --cask arduino-ide          # só pra serial monitor, se quiser
brew install esptool
```

TinyGo v0.41+ flasha ESP32 direto, sem esptool externo, via `espflasher`:
```bash
tinygo flash -target=xiao-esp32s3 ./cmd/firmware
```

Se for pro caminho C, ESP-IDF instala com o script oficial e funciona em Apple Silicon.

## Realidade sobre prazo

Firmware embarcado com display leva 3x mais tempo do que você estima.
A primeira vez que um sprite animar direito na tela vai levar uns 3 fins de semana.
Isso é normal e não é sinal de que o projeto está errado.

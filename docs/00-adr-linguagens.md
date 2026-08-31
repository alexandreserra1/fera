# ADR 001: Escolha de linguagens

Status: aceito
Data: 2026-08

## A pergunta

Go? C? Rust? C++? A resposta honesta é: depende da camada, e a maioria das camadas
não se beneficia em nada de linguagem de baixo nível.

## Decisão por camada

### Backend: Go

Sem discussão. O backend é ingest de eventos, fold de estado e serve JSON.
Isso é I/O bound. C, C++ ou Rust não entregam nada aqui além de dívida de tempo.
Go dá: goroutines, `net/http` decente, deploy de binário único, GC previsível
o suficiente pra essa carga, e é o que você quer no currículo.

### Núcleo de simulação: Go puro, zero dependências

O `internal/sim` é uma função pura. Sem clock, sem rede, sem banco.
Isso permite compilar o **mesmo core** para:

- servidor (Go normal)
- web/app (TinyGo -> WASM)
- device (TinyGo -> ESP32-S3, desde a v0.41 com WiFi nativo)

Um core, três alvos. É o que resolve "código reutilizável" de verdade,
e não com interface enfeitada.

### Firmware: dois caminhos, escolha consciente

**Caminho A: ESP-IDF em C + LVGL.** Maduro. Drivers de display prontos, LVGL,
animação, touch, tudo funciona hoje. É onde toda a comunidade está.
Custo: você reimplementa o `sim` em C, e agora tem duas implementações
que precisam concordar. Mitigação: testes de conformidade com vetores dourados
(golden vectors) gerados pelo core Go.

**Caminho B: TinyGo direto no ESP32-S3.** Desde a v0.41 tem WiFi nativo via
`espradio`, flash sem ferramenta externa via `espflasher`, e ESP32-S3 é alvo
suportado. Você roda o **mesmo** `internal/sim` no device. Zero divergência.
Custo: ecossistema de display bem mais magro que LVGL. Você vai escrever
mais driver na mão.

**Recomendação:** comece pelo B com display simples (SSD1306 OLED mono ou
ST7789 240x240, ambos têm driver em `tinygo.org/x/drivers`). Se bater na parede
de gráficos, migra pro A com os golden vectors já prontos como rede de segurança.
A decisão fica reversível, que é o que importa.

### Rust

Rust seria a escolha certa se o core precisasse ser `no_std` e compartilhado
via FFI com C. É uma arquitetura defensável e mais robusta a longo prazo.
Não é a certa **pra você agora**: adiciona uma linguagem nova a um projeto
que você quer terminar por diversão. Se o TinyGo falhar E o C incomodar,
`esp-rs` é o plano C.

### C++

Não. Nenhuma vantagem sobre C no ESP-IDF pra esse escopo, e mais complexidade.

### App: TypeScript + Expo (React Native)

iOS, Android e web numa base só, roda tudo do MacBook M1.
Você não é frontend e não precisa ser. Expo esconde o build nativo.
O core de regras vai como WASM (TinyGo), então o app não reimplementa nada.

## Consequência

Você escreve as regras do bicho **uma vez**, em Go, e elas são idênticas
no servidor, no app e no dispositivo. Esse é o pilar do design inteiro.
Se alguma decisão futura quebrar essa propriedade, ela está errada.

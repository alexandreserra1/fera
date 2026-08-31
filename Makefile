.PHONY: test cover lint check check-all device tidy vectors frames telas bicho bicho-api tamanho web wasm

# Precisa de Docker: internal/repo sobe um Postgres de verdade com
# testcontainers. Mockar pgx testaria o mock, não o ON CONFLICT.
test:
	go test ./... -race -count=1

# Mede só ./internal/sim, sem o /... : o pacote vectors é andaime de teste e
# arrastaria o total pra baixo escondendo a cobertura real do core.
cover:
	go test ./internal/sim -coverprofile=c.out
	@go tool cover -func=c.out | grep total | awk '{gsub("%","",$$3); if ($$3+0 < 90) {print "FALHOU: cobertura do sim em " $$3 "%, mínimo 90%"; exit 1} else {print "OK: cobertura do sim " $$3 "%"}}'
	@rm -f c.out

lint:
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...

# O contrato entre runtimes: os mesmos golden vectors rodando sob TinyGo, e o
# PROGRAMA INTEIRO do device linkando no Xtensa. Falha alto se o tinygo não
# estiver instalado, em vez de passar em silêncio: skip silencioso aqui é o
# mesmo que não ter contrato nenhum.
device:
	@command -v tinygo >/dev/null || { echo "FALHOU: tinygo não instalado (brew install tinygo-org/tools/tinygo)"; exit 1; }
	tinygo test ./internal/sim
	tinygo build -o /dev/null -target=xiao-esp32s3 ./cmd/firmware

# Regenerar vetor é ato deliberado. Se uma regra do sim mudou de propósito,
# suba o SchemaVer ANTES de rodar isto.
vectors:
	go run ./cmd/gen-vectors -write

# Telas douradas do device. Confira o DIFF antes de aceitar: mudar o desenho
# é decisão, não efeito colateral de um refactor.
frames:
	go run ./cmd/gen-frames -write

# Desenha as telas no terminal, sem gravar nada.
telas:
	@go run ./cmd/gen-frames

# A FERA no terminal. Bancada de calibragem, não demo: é aqui que dá pra
# sentir se o balanceamento do sim faz sentido.
bicho:
	go run ./cmd/feradev -velocidade=1800

# O mesmo device, sincronizando de verdade. Precisa da API de pé (ver README).
bicho-api:
	go run ./cmd/feradev -velocidade=86400 -api=http://localhost:8080

# A FERA no navegador: o mesmo sim, ui e display compilados pra WASM.
web:
	tinygo build -o web/fera.wasm -target=wasm ./cmd/wasm
	@echo "abra http://localhost:8000"
	@cd web && python3 -m http.server 8000

# Só compila o wasm, sem servir.
wasm:
	tinygo build -o web/fera.wasm -target=wasm ./cmd/wasm
	@ls -lh web/fera.wasm | awk '{print "web/fera.wasm:", $$5}'

# Quanto o programa do device ocupa no alvo real.
tamanho:
	@tinygo build -o /dev/null -size=short -target=xiao-esp32s3 ./cmd/firmware

check: lint test cover
check-all: check device

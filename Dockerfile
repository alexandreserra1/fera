# Binário único, sem runtime, sem shell. É o que o docs/00 promete do Go:
# "deploy de binário único".
FROM golang:1.26-alpine AS build
WORKDIR /src
# go.mod primeiro: a camada de dependências só refaz quando elas mudam.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO desligado e binário estático, pra caber num scratch.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /api /api
# As migrações vão junto: quem sobe o container precisa delas à mão, e o
# data-layer proíbe migração automática no boot.
COPY --from=build /src/migrations /migrations
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]

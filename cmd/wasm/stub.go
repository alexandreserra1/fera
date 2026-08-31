//go:build !wasm

// Este pacote só existe pro alvo wasm. O stub mantém `go build ./...` e
// `go vet ./...` funcionando no host, onde o syscall/js não existe.
package main

import "fmt"

func main() {
	fmt.Println("cmd/wasm só compila pro navegador: make web")
}

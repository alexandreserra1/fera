package service

import "errors"

// Três sentinelas, que é o que o HTTP precisa distinguir. Todo o resto é
// %w com contexto e vira 500. Se um dia virar sete, provavelmente a borda
// está tentando explicar detalhe interno pro cliente.
//
// Hoje só ErrForbidden é PRODUZIDO (auth de device, seis caminhos). Os outros
// dois existem como contrato de mapeamento, exercitado em
// TestSentinelaDoServiceViraStatus, e ainda não têm quem os retorne:
//
//   - ErrNotFound: pet inexistente não chega ao service. O requireOwnPet
//     barra antes, com 404, porque um pet que não é seu e um que não existe
//     têm que ser indistinguíveis de fora.
//   - ErrConflict: o api-contract proíbe 409 pra duplicata em ingest, que
//     seria o candidato óbvio. Nenhum outro caso apareceu ainda.
var (
	ErrNotFound  = errors.New("não encontrado")
	ErrForbidden = errors.New("proibido")
	ErrConflict  = errors.New("conflito")
)

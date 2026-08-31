---
name: fera-context
description: Contexto mestre do projeto FERA. Carregue SEMPRE antes de escrever qualquer código, tomar decisão de arquitetura, ou responder pergunta sobre o projeto. Contém os invariantes que nenhuma outra decisão pode quebrar.
---

# Contexto FERA

## O que é

Criatura digital que evolui a partir de esforço físico real do dono.
Roda num dispositivo ESP32-S3 de bolso, num app, e tem backend Go.
Projeto pessoal, feito por diversão, com padrão de código de produção.

## Os cinco invariantes

Se qualquer sugestão sua violar um destes, pare e avise antes de escrever código.

1. **`internal/sim` é puro.** Zero dependências fora da stdlib. Sem `time.Now()`,
   sem `rand` sem seed explícito, sem iteração de map sem ordenação, sem I/O.
   Tempo entra como parâmetro. Aleatoriedade entra como seed determinístico.

2. **O estado é derivado, nunca escrito direto.** `estado = Fold(genesis, eventos, t)`.
   Nunca existe um `UPDATE pets SET vigor = ...`. Se você quer mudar estado, emite evento.

3. **Todo evento tem ULID gerado no cliente.** Insert é `ON CONFLICT DO NOTHING`.
   Nunca crie tabela de idempotency key, nunca use Redis pra isso.

4. **Nenhum JOIN em caminho de leitura quente.** Precisa de uma view nova? Cria projeção.

5. **Teste antes do código.** Sempre. Sem exceção. Ver skill `tdd-guard`.

## Estilo de código esperado

- Funções curtas. Se passou de 40 linhas, quebra.
- Sem framework de DI. Wiring explícito no `main`.
- Sem ORM. `pgx` com SQL escrito à mão.
- Erros: `fmt.Errorf("contexto: %w", err)`. Sentinelas só quando o chamador precisa decidir.
- `context.Context` como primeiro parâmetro em tudo que faz I/O.
- Comentário explica **por quê**, nunca **o quê**.
- Nome de variável curto em escopo curto (`i`, `ev`), longo em escopo longo.

## O que NÃO fazer neste projeto

- Não sugerir Kubernetes, service mesh, Kafka, ou microserviços. É um binário.
- Não sugerir API gateway de terceiro (Kong, Tyk). Caddy + middleware Go resolve.
- Não criar interface com mais de 4 métodos.
- Não criar pacote `utils`, `helpers`, `common` ou `models`.
- Não gerar 800 linhas de uma vez. Uma fatia vertical por vez, com teste.
- Não usar `panic` fora de `main` e de inicialização.

## Perfil do dev

Backend/data engineer, ~5 anos, Go e Python. Sabe Clean Architecture, Kafka,
sistemas distribuídos. Não precisa de explicação de conceito básico.
Prefere resposta direta a resposta diplomática. Se a ideia dele for ruim, diga.
Escreve em português brasileiro informal. Não use travessão em texto corrido.

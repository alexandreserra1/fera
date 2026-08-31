package ui

import (
	"strings"
	"testing"

	"github.com/ale/fera/internal/sim"
)

// Contar caractere na mão é como se escreve arte torta. Este teste confere as
// dimensões pelo texto, então uma linha curta ou sobrando falha na hora em vez
// de virar sprite deslocado na tela.
func TestArteTemAsDimensoesDeclaradas(t *testing.T) {
	for _, c := range []struct {
		nome string
		arte string
	}{
		{"ovo", arteOvo}, {"filhote", arteFilhote}, {"jovem", arteJovem},
		{"adulto", arteAdulto}, {"veterano", arteVeterano},
	} {
		t.Run(c.nome, func(t *testing.T) {
			linhas := strings.Split(strings.TrimSpace(c.arte), "\n")
			if len(linhas) != alturaSprite {
				t.Fatalf("%d linhas, esperado %d", len(linhas), alturaSprite)
			}
			for i, l := range linhas {
				if len(l) != larguraSprite {
					t.Errorf("linha %d tem %d caracteres, esperado %d: %q",
						i, len(l), larguraSprite, l)
				}
				if n := strings.Trim(l, ".#:"); n != "" {
					t.Errorf("linha %d tem caractere inesperado %q", i, n)
				}
			}
		})
	}
}

// Bicho torto de um lado só é erro de digitação, não escolha de design.
//
// A exceção é declarada, não implícita: as linhas 7 e 8 do ovo são a rachadura,
// e rachadura simétrica lê como faixa decorativa em vez de casca quebrando.
// Assimetria nova em qualquer outro lugar continua sendo erro.
func TestBichosSaoSimetricos(t *testing.T) {
	// Hoje todos são simétricos: a costura do ovo virou uma fenda centralizada
	// em vez de ziguezague. O mapa fica pro dia em que algum bicho for
	// assimétrico DE PROPÓSITO, e o teste cobra que a exceção seja verdadeira.
	assimetriaProposital := map[string]map[int]bool{}

	for _, c := range []struct{ nome, arte string }{
		{"ovo", arteOvo}, {"filhote", arteFilhote}, {"jovem", arteJovem},
		{"adulto", arteAdulto}, {"veterano", arteVeterano},
	} {
		t.Run(c.nome, func(t *testing.T) {
			for i, l := range strings.Split(strings.TrimSpace(c.arte), "\n") {
				if assimetriaProposital[c.nome][i] {
					if l == inverte(l) {
						t.Errorf("linha %d está marcada como assimétrica mas é simétrica: "+
							"tire a exceção", i)
					}
					continue
				}
				if l != inverte(l) {
					t.Errorf("linha %d não é simétrica:\n  %s\n  %s", i, l, inverte(l))
				}
			}
		})
	}
}

func inverte(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// Todo estágio do sim precisa de arte. Adicionar estágio novo sem desenhar o
// bicho deixaria a tela vazia sem nenhum erro.
func TestTodoEstagioTemSprite(t *testing.T) {
	for s := sim.StageOvo; s <= sim.StageVeterano; s++ {
		sp := SpriteDoEstagio(s)
		if sp.W != larguraSprite || sp.H != alturaSprite {
			t.Errorf("estágio %d: sprite %dx%d", s, sp.W, sp.H)
		}
		var acesos int
		for y := int16(0); y < sp.H; y++ {
			for x := int16(0); x < sp.W; x++ {
				if sp.At(x, y) {
					acesos++
				}
			}
		}
		if acesos < 50 {
			t.Errorf("estágio %d tem só %d pixels acesos: arte faltando?", s, acesos)
		}
	}
}

// Estágio fora da faixa não pode dar pânico: no device isso é tela morta sem
// nenhuma pista do motivo.
func TestEstagioForaDaFaixaCaiNoOvo(t *testing.T) {
	if got := SpriteDoEstagio(sim.Stage(99)); got.Bits == nil {
		t.Fatal("estágio inválido devolveu sprite vazio em vez do ovo")
	}
}

// O dither é o terceiro tom: um bit por pixel dá preto e branco, e o xadrez
// dá o cinza que faz a arte parecer pixel art em vez de mancha.
func TestDitherViraXadrez(t *testing.T) {
	sp := empacota("::::\n::::\n::::\n::::", 4, 4)

	var acesos int
	for y := int16(0); y < 4; y++ {
		for x := int16(0); x < 4; x++ {
			esperado := (x+y)%2 == 0
			if sp.At(x, y) != esperado {
				t.Errorf("(%d,%d) = %v, esperado %v: o xadrez saiu errado", x, y, sp.At(x, y), esperado)
			}
			if sp.At(x, y) {
				acesos++
			}
		}
	}
	if acesos != 8 {
		t.Errorf("%d de 16 pixels acesos, esperado 8 (metade)", acesos)
	}
}

// Sólido continua sólido e vazio continua vazio: o dither não pode contaminar
// os outros dois tons.
func TestOsTresTonsSaoDistintos(t *testing.T) {
	sp := empacota("####\n::::\n....\n####", 4, 4)
	for x := int16(0); x < 4; x++ {
		if !sp.At(x, 0) || !sp.At(x, 3) {
			t.Errorf("linha sólida tem buraco em x=%d", x)
		}
		if sp.At(x, 2) {
			t.Errorf("linha vazia tem pixel em x=%d", x)
		}
	}
	var meio int
	for x := int16(0); x < 4; x++ {
		if sp.At(x, 1) {
			meio++
		}
	}
	if meio != 2 {
		t.Errorf("linha de dither com %d de 4 acesos, esperado 2", meio)
	}
}

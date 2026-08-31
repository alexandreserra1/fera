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

// O teste de simetria que existia aqui foi APOSENTADO, não apagado por estar
// vermelho. Ele pegava erro de digitação na arte escrita à mão; a arte agora
// vem importada de fonte verificada (Kenney, CC0) com teste de ida e volta no
// próprio cmd/import-sprite, e "typo" deixou de ser um modo de falha possível.
// Mantê-lo significaria carregar uma lista de números de linha por bicho que
// não protege nada.
//
// O modo de falha de HOJE é outro: pegar a célula errada do tilesheet. Um
// off-by-one na grade traz meio bicho, ou o vizinho, ou espaço vazio. É isso
// que os dois testes abaixo pegam.

// Bicho colado numa borda é célula errada: o recorte pegou metade de um e
// metade do vizinho.
func TestBichoEstaCentradoNaArte(t *testing.T) {
	for _, c := range []struct{ nome, arte string }{
		{"ovo", arteOvo}, {"filhote", arteFilhote}, {"jovem", arteJovem},
		{"adulto", arteAdulto}, {"veterano", arteVeterano},
	} {
		t.Run(c.nome, func(t *testing.T) {
			linhas := strings.Split(strings.TrimSpace(c.arte), "\n")
			esq, dir := int16(larguraSprite), int16(0)
			for _, l := range linhas {
				for x, ch := range l {
					if ch == '#' || ch == ':' {
						if int16(x) < esq {
							esq = int16(x)
						}
						if int16(x) > dir {
							dir = int16(x)
						}
					}
				}
			}
			if esq > dir {
				t.Fatal("arte vazia")
			}
			// as margens dos dois lados têm que ser parecidas
			margemDir := int16(larguraSprite) - 1 - dir
			if d := esq - margemDir; d > 6 || d < -6 {
				t.Errorf("margens desiguais (%d à esquerda, %d à direita): "+
					"o recorte pegou a célula errada do tilesheet?", esq, margemDir)
			}
		})
	}
}

// Bicho tem que ocupar uma fatia razoável do quadro. Pouco demais é célula
// vazia; demais é um tile de parede em vez de criatura.
func TestBichoOcupaFatiaRazoavelDoQuadro(t *testing.T) {
	total := int(larguraSprite) * int(alturaSprite)
	for s := sim.StageOvo; s <= sim.StageVeterano; s++ {
		sp := SpriteDoEstagio(s)
		var acesos int
		for y := int16(0); y < sp.H; y++ {
			for x := int16(0); x < sp.W; x++ {
				if sp.At(x, y) {
					acesos++
				}
			}
		}
		pct := acesos * 100 / total
		if pct < 10 {
			t.Errorf("estágio %d ocupa só %d%% do quadro: célula vazia?", s, pct)
		}
		if pct > 70 {
			t.Errorf("estágio %d ocupa %d%% do quadro: pegou um tile de bloco?", s, pct)
		}
	}
}

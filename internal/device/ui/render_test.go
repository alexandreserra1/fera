package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ale/fera/internal/device/display"
	"github.com/ale/fera/internal/sim"
)

// As telas em testdata/ são o contrato visual. Se este teste quebrar, ou você
// mudou o desenho de propósito (aí `make frames` e confira o diff), ou
// alguma coisa no render saiu do lugar sem você perceber.
//
// Roda no Mac, sem placa. Uma placa ligada nunca precisou entrar nisto.
func TestGoldenFrames(t *testing.T) {
	arquivos, err := filepath.Glob("testdata/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(arquivos) == 0 {
		t.Fatal("nenhuma tela em testdata/; rode go run ./cmd/gen-frames -write")
	}

	porNome := map[string]sim.View{}
	for _, f := range Frames() {
		porNome[f.Nome] = f.View
	}

	for _, arq := range arquivos {
		nome := strings.TrimSuffix(filepath.Base(arq), ".txt")
		t.Run(nome, func(t *testing.T) {
			v, ok := porNome[nome]
			if !ok {
				t.Fatalf("tela órfã: %s não está no catálogo Frames()", nome)
			}
			want, err := os.ReadFile(arq)
			if err != nil {
				t.Fatal(err)
			}

			b := display.NewBuffer(Largura, Altura)
			Render(b, v)

			if got := b.String(); got != string(want) {
				t.Errorf("a tela mudou\n--- desenhado ---\n%s\n--- esperado ---\n%s",
					got, want)
			}
		})
	}

	if len(arquivos) != len(Frames()) {
		t.Errorf("%d arquivos pra %d telas no catálogo", len(arquivos), len(Frames()))
	}
}

// A regra de alocação zero do skill firmware, virada teste. Se o render passar
// a alocar, o GC conservativo do TinyGo acorda num device que deveria estar
// dormindo, e o consumo médio de 0,7 mA do docs/06 deixa de fechar.
func TestRenderNaoAloca(t *testing.T) {
	b := display.NewBuffer(Largura, Altura)
	v := Frames()[3].View

	if n := testing.AllocsPerRun(200, func() { Render(b, v) }); n != 0 {
		t.Errorf("Render alocou %v vezes por frame, esperado 0", n)
	}
}

// Bicho zerado tem que ser distinguível de tela quebrada. A moldura da barra
// existe pra isso: ela aparece mesmo com valor zero.
func TestBarraZeradaAindaMostraMoldura(t *testing.T) {
	b := display.NewBuffer(Largura, Altura)
	Render(b, sim.View{Stage: sim.StageAdulto, Trait: sim.TraitNeutro})

	var acesos int
	for x := barraX; x < barraX+barraW; x++ {
		if b.Get(int16(x), primeiraY-1) {
			acesos++
		}
	}
	if acesos != barraW {
		t.Errorf("a moldura da barra zerada tem %d de %d pixels", acesos, barraW)
	}
}

// Valor acima do teto não pode transbordar a moldura e virar lixo na tela.
func TestBarraNaoTransbordaAMoldura(t *testing.T) {
	b := display.NewBuffer(Largura, Altura)
	Render(b, sim.View{
		Stage: sim.StageAdulto,
		Stats: sim.Stats{Vigor: sim.Max * 3, Animo: -500, Saude: sim.Max, Vinculo: 0},
	})

	// a coluna logo depois da moldura tem que continuar apagada
	for y := int16(0); y < Altura; y++ {
		if b.Get(barraX+barraW, y) {
			t.Fatalf("a barra passou da moldura na linha %d", y)
		}
	}
}

// Todo caractere que o renderer coloca na tela precisa de glifo. Sem isto, um
// traço novo no sim apareceria como buraco no meio da palavra e ninguém
// perceberia até olhar a placa.
func TestTodoCaractereRenderizadoTemGlifo(t *testing.T) {
	var usados []string
	for s := sim.StageOvo; s <= sim.StageVeterano; s++ {
		usados = append(usados, NomeDoEstagio(s))
	}
	for tr := sim.TraitNeutro; tr <= sim.TraitFerino; tr++ {
		usados = append(usados, NomeDoTraco(tr))
	}
	usados = append(usados, "VIG", "ANI", "SAU", "VIN", "0123456789")

	for _, s := range usados {
		for i := 0; i < len(s); i++ {
			if s[i] == ' ' {
				continue
			}
			if glifos[s[i]] == ([alturaGlifo]byte{}) {
				t.Errorf("o caractere %q de %q não tem glifo", s[i], s)
			}
		}
	}
}

// Nome de estágio e de traço fora da faixa não pode dar pânico no device.
func TestNomesForaDaFaixaNaoQuebram(t *testing.T) {
	if NomeDoEstagio(sim.Stage(99)) == "" || NomeDoTraco(sim.Trait(99)) == "" {
		t.Error("valor fora da faixa devolveu nome vazio")
	}
}

// A fonte e os bichos são empacotados no init a partir de arte em ASCII, o que
// os coloca na RAM em vez da flash. A troca é deliberada (hex à mão é
// ilegível e cheio de typo silencioso) mas precisa de teto: arte inchando sem
// ninguém olhar é como orçamento de RAM morre.
//
// O teto subiu de 2 KB pra 4 KB quando a arte foi de 32x32 pra 64x64, e o
// aumento foi MEDIDO, não estimado. `tinygo build -size -target=xiao-esp32s3
// ./cmd/firmware` antes e depois:
//
//	32x32:  137937 flash / 56468 RAM
//	64x64:  137937 flash / 63652 RAM   (+7,2 KB)
//
// São 63 KB dos 512 KB de SRAM do ESP32-S3, com o stack de WiFi (~50 KB) ainda
// por cima. Cabe com folga, e a arte é a identidade visual do projeto.
func TestOrcamentoDeRAMDaUI(t *testing.T) {
	const teto = 4096

	fonte := len(glifos) * alturaGlifo
	var arte int
	for _, s := range sprites {
		arte += len(s.Bits)
	}
	total := fonte + arte

	t.Logf("fonte %d B + sprites %d B = %d B (teto %d)", fonte, arte, total, teto)
	if total > teto {
		t.Errorf("a UI passou a ocupar %d B de RAM, teto %d", total, teto)
	}
}

// O framebuffer é o maior bloco de RAM do firmware e o docs/06 orça 1 KB pro
// SSD1306. Mudar a resolução do render muda esse número.
func TestFramebufferCabeNoOrcamento(t *testing.T) {
	if got := len(display.NewBuffer(Largura, Altura).Bits); got != 1024 {
		t.Errorf("framebuffer de %d B, o docs/06 orça 1024", got)
	}
}

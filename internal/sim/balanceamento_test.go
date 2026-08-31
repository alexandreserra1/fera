package sim

import (
	"fmt"
	"testing"
	"time"
)

// Bancada de balanceamento.
//
// Todo valor de DefaultTuning é chute marcado com "TODO: calibrar", e chute
// não se corrige olhando a tela: se corrige definindo como o bicho DEVE
// responder a padrões de treino reais e conferindo se responde.
//
// Este arquivo transforma "o bicho definha se você não treina" em aritmética.

// persona é um padrão de vida de 90 dias.
type persona struct {
	nome string
	// treina devolve (kcal, zona) do dia, ou kcal 0 se não treinou.
	treina func(dia int) (uint16, uint8)
	// dormeMin é quanto o dono dorme por noite. 0 significa que não registra.
	dormeMin uint16
	// interageDia diz se o dono aperta o botão naquele dia.
	interage func(dia int) bool
}

func personas() []persona {
	sempre := func(int) bool { return true }
	nunca := func(int) bool { return false }

	return []persona{
		{
			nome:     "sedentário (nunca treina, registra sono)",
			treina:   func(int) (uint16, uint8) { return 0, 0 },
			dormeMin: 450,
			interage: sempre,
		},
		{
			nome: "iniciante (2x/semana, leve)",
			treina: func(d int) (uint16, uint8) {
				if d%7 == 1 || d%7 == 4 {
					return 300, 2
				}
				return 0, 0
			},
			dormeMin: 440,
			interage: sempre,
		},
		{
			nome: "constante (3x/semana, moderado)",
			treina: func(d int) (uint16, uint8) {
				if d%7 == 1 || d%7 == 3 || d%7 == 5 {
					return 500, 3
				}
				return 0, 0
			},
			dormeMin: 450,
			interage: sempre,
		},
		{
			nome: "atleta (6x/semana, forte, dorme bem)",
			treina: func(d int) (uint16, uint8) {
				if d%7 != 0 {
					return 700, 4
				}
				return 0, 0
			},
			dormeMin: 480,
			interage: sempre,
		},
		{
			nome: "overtraining (6x/semana no talo, dorme mal)",
			treina: func(d int) (uint16, uint8) {
				if d%7 != 0 {
					return 900, 5
				}
				return 0, 0
			},
			dormeMin: 300,
			interage: nunca,
		},
		{
			nome: "sumiu (treina 2 semanas e para)",
			treina: func(d int) (uint16, uint8) {
				if d < 14 && (d%7 == 1 || d%7 == 3 || d%7 == 5) {
					return 500, 3
				}
				return 0, 0
			},
			dormeMin: 0,
			interage: func(d int) bool { return d < 14 },
		},
	}
}

// viver roda a persona por n dias e devolve o estado a cada marco.
func viver(p persona, dias int, t Tuning) (marcos map[int]View, quandoVirou map[Stage]int) {
	inicio := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	s := Genesis("pet", inicio)
	marcos = map[int]View{}
	quandoVirou = map[Stage]int{StageOvo: 0}

	seq := 0
	id := func() string { seq++; return fmt.Sprintf("%08d", seq) }

	for d := 0; d < dias; d++ {
		dia := inicio.AddDate(0, 0, d)
		var evs []Event

		if kcal, zona := p.treina(d); kcal > 0 {
			evs = append(evs, Event{
				ID: id(), At: dia.Add(7 * time.Hour),
				Kind: KindEffort, Kcal: kcal, Zone: zona,
			})
		}
		if p.dormeMin > 0 {
			evs = append(evs, Event{
				ID: id(), At: dia.Add(23 * time.Hour),
				Kind: KindSleep, Minutes: p.dormeMin,
			})
		}
		if p.interage(d) {
			evs = append(evs, Event{
				ID: id(), At: dia.Add(20 * time.Hour), Kind: KindInteract,
			})
		}

		antes := s.Stage
		s = Fold(s, evs, t)
		if s.Stage != antes {
			if _, visto := quandoVirou[s.Stage]; !visto {
				quandoVirou[s.Stage] = d
			}
		}
		if d == 6 || d == 29 || d == 59 || d == 89 {
			marcos[d+1] = Project(s, dia.Add(23*time.Hour+59*time.Minute), t)
		}
	}
	return marcos, quandoVirou
}

var nomeEstagio = map[Stage]string{
	StageOvo: "ovo", StageFilhote: "filhote", StageJovem: "jovem",
	StageAdulto: "adulto", StageVeterano: "veterano",
}

// Não é asserção: é a fotografia do balanceamento atual.
// `go test ./internal/sim -run TestBalanceamentoAtual -v`
func TestBalanceamentoAtual(t *testing.T) {
	tn := DefaultTuning()

	for _, p := range personas() {
		marcos, virou := viver(p, 90, tn)
		t.Logf("\n=== %s ===", p.nome)
		t.Logf("  %-8s %-10s %5s %5s %5s %5s", "dia", "estágio", "VIG", "ANI", "SAU", "VIN")
		for _, d := range []int{7, 30, 60, 90} {
			v := marcos[d]
			t.Logf("  %-8d %-10s %5d %5d %5d %5d", d, nomeEstagio[v.Stage],
				Pct(v.Stats.Vigor), Pct(v.Stats.Animo), Pct(v.Stats.Saude), Pct(v.Stats.Vinculo))
		}
		var linha string
		for _, st := range []Stage{StageFilhote, StageJovem, StageAdulto, StageVeterano} {
			if d, ok := virou[st]; ok {
				linha += fmt.Sprintf("  %s: dia %d", nomeEstagio[st], d)
			}
		}
		if linha == "" {
			linha = "  nunca saiu de ovo"
		}
		t.Logf(" evoluções:%s", linha)
	}
}

// ---- os alvos, como asserção ----
//
// Daqui pra baixo o balanceamento deixa de ser opinião. Cada teste é uma
// frase do README ou uma decisão de produto, escrita de um jeito que falha
// quando o número deixa de sustentá-la.

// A primeira linha do README: "só cresce com esforço físico real" e "não dá
// pra alimentar apertando botão". Dormir e apertar botão mexem nos atributos,
// mas NÃO fazem o bicho crescer.
func TestSoEsforcoFazCrescer(t *testing.T) {
	tn := DefaultTuning()
	base := Genesis("pet", t0)

	casos := []struct {
		nome   string
		ev     Event
		cresce bool
	}{
		{"esforço", Event{ID: "a", At: t0.Add(time.Hour), Kind: KindEffort, Kcal: 500, Zone: 3}, true},
		{"encontro", Event{ID: "a", At: t0.Add(time.Hour), Kind: KindEncounter, PeerID: "p"}, true},
		{"sono", Event{ID: "a", At: t0.Add(time.Hour), Kind: KindSleep, Minutes: 450}, false},
		{"botão", Event{ID: "a", At: t0.Add(time.Hour), Kind: KindInteract}, false},
	}
	for _, c := range casos {
		got := Fold(base, []Event{c.ev}, tn).Growth
		if c.cresce && got == 0 {
			t.Errorf("%s não fez o bicho crescer", c.nome)
		}
		if !c.cresce && got != 0 {
			t.Errorf("%s fez o bicho crescer %d: o README diz que só esforço faz", c.nome, got)
		}
	}
}

// A consequência disso: quem não treina não evolui, por mais que durma e
// aperte botão todo dia.
func TestSedentarioNaoEvolui(t *testing.T) {
	_, virou := viver(personas()[0], 365, DefaultTuning())
	if len(virou) != 1 {
		t.Errorf("o sedentário evoluiu em %v: só esforço deveria fazer crescer", virou)
	}
}

// O ritmo decidido: filhote em ~1 semana, jovem em ~3 semanas, adulto em
// ~3 meses e veterano em ~1 ano, pra quem treina 3x por semana.
func TestRitmoDeEvolucaoDoDonoConstante(t *testing.T) {
	_, virou := viver(personas()[2], 400, DefaultTuning())

	alvos := []struct {
		estagio    Stage
		diaAlvo    int
		tolerancia int
	}{
		{StageFilhote, 9, 5},
		{StageJovem, 23, 10},
		{StageAdulto, 93, 30},
		{StageVeterano, 350, 90},
	}
	for _, a := range alvos {
		dia, ok := virou[a.estagio]
		if !ok {
			t.Errorf("nunca virou %s em 400 dias de treino 3x/semana", nomeEstagio[a.estagio])
			continue
		}
		if d := dia - a.diaAlvo; d > a.tolerancia || d < -a.tolerancia {
			t.Errorf("%s no dia %d, alvo %d (±%d)", nomeEstagio[a.estagio], dia, a.diaAlvo, a.tolerancia)
		}
	}
}

// Vigor reflete treino recente. Se nem o atleta consegue vigor, o atributo
// não mede nada: era o estado do balanceamento antes desta calibragem.
func TestVigorRefleteOTreinoRecente(t *testing.T) {
	tn := DefaultTuning()
	nomes := []string{"sedentário", "iniciante", "constante", "atleta"}
	var vig []uint8
	for i, p := range personas()[:4] {
		m, _ := viver(p, 90, tn)
		vig = append(vig, Pct(m[90].Stats.Vigor))
		t.Logf("%-12s vigor %d", nomes[i], vig[i])
	}

	if vig[0] != 0 {
		t.Errorf("sedentário terminou com vigor %d, esperado 0", vig[0])
	}
	if vig[1] < 20 {
		t.Errorf("iniciante terminou com vigor %d: treinar 2x/semana tem que aparecer", vig[1])
	}
	if vig[2] <= vig[1] {
		t.Errorf("constante (%d) não passou do iniciante (%d)", vig[2], vig[1])
	}
	if vig[3] < 90 {
		t.Errorf("atleta terminou com vigor %d, esperado quase no talo", vig[3])
	}
}

// "Se você treina demais e não dorme, ela fica arisca." Saúde é o balanço
// entre carga e descanso: o atleta que dorme bem se sustenta, o que dorme mal
// definha, mesmo treinando quase igual.
func TestOvertrainingDerrubaASaudeMasTreinarBemNao(t *testing.T) {
	tn := DefaultTuning()
	atleta, _ := viver(personas()[3], 90, tn)
	overt, _ := viver(personas()[4], 90, tn)

	sAtleta := Pct(atleta[90].Stats.Saude)
	sOvert := Pct(overt[90].Stats.Saude)
	t.Logf("saúde aos 90 dias: atleta %d, overtraining %d", sAtleta, sOvert)

	if sAtleta < 70 {
		t.Errorf("o atleta que dorme bem terminou com saúde %d: treinar forte com descanso tem que sustentar", sAtleta)
	}
	if sOvert > 40 {
		t.Errorf("o overtraining terminou com saúde %d: treinar no talo sem dormir tem que cobrar", sOvert)
	}
}

// "Se você não treina, ela definha." Quem para tem que ver diferença em
// dias, não em meses, mas o bicho não pode zerar da noite pro dia.
func TestQuemParaVeADiferencaEmDias(t *testing.T) {
	tn := DefaultTuning()
	m, _ := viver(personas()[5], 90, tn) // treina 2 semanas e para

	v7, v30, v60 := Pct(m[7].Stats.Vigor), Pct(m[30].Stats.Vigor), Pct(m[60].Stats.Vigor)
	t.Logf("vigor: dia 7 (treinando) = %d, dia 30 = %d, dia 60 = %d", v7, v30, v60)

	if v7 < 20 {
		t.Errorf("na 1a semana, treinando, o vigor era %d: baixo demais pra depois cair", v7)
	}
	if v30 >= v7/2 {
		t.Errorf("duas semanas depois de parar o vigor era %d, mais da metade dos %d de quando treinava", v30, v7)
	}
	if v60 != 0 {
		t.Errorf("mais de um mês largado e o vigor era %d, esperado 0", v60)
	}
	if Pct(m[30].Stats.Animo) > 20 {
		t.Errorf("o ânimo aos 30 dias era %d: abandono tem que aparecer", Pct(m[30].Stats.Animo))
	}
}

// Vínculo é o atributo LENTO: sobe com consistência, não com um dia bom.
// É o que distingue quem cuida do bicho todo dia de quem lembra dele às vezes.
func TestVinculoSobeComConsistencia(t *testing.T) {
	tn := DefaultTuning()
	m, _ := viver(personas()[2], 90, tn) // interage todo dia

	v30, v90 := Pct(m[30].Stats.Vinculo), Pct(m[90].Stats.Vinculo)
	t.Logf("vínculo: dia 30 = %d, dia 90 = %d", v30, v90)

	if v30 == 0 {
		t.Error("um mês interagindo todo dia não construiu vínculo nenhum")
	}
	if v90 <= v30 {
		t.Errorf("o vínculo não cresceu do dia 30 (%d) ao 90 (%d)", v30, v90)
	}
	if v30 > 60 {
		t.Errorf("vínculo em %d com um mês: rápido demais pro atributo que mede consistência", v30)
	}
}

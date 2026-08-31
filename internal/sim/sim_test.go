package sim

import (
	"math/rand"
	"testing"
	"time"
)

var (
	t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	tn = Tuning{}
)

func init() { tn = DefaultTuning() }

func evs() []Event {
	return []Event{
		{ID: "01A", At: t0.Add(1 * time.Hour), Kind: KindEffort, Kcal: 500, Zone: 3},
		{ID: "01B", At: t0.Add(2 * time.Hour), Kind: KindInteract},
		{ID: "01C", At: t0.Add(3 * time.Hour), Kind: KindSleep, Minutes: 420},
		{ID: "01D", At: t0.Add(4 * time.Hour), Kind: KindEncounter, PeerID: "p2"},
	}
}

// Fold precisa dar o mesmo resultado independente da ordem de chegada.
// Se este teste quebrar, device e servidor divergem e o sistema inteiro cai.
func TestFoldEhComutativo(t *testing.T) {
	want := Fold(Genesis("pet1", t0), evs(), tn)

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 200; i++ {
		e := evs()
		rng.Shuffle(len(e), func(a, b int) { e[a], e[b] = e[b], e[a] })
		if got := Fold(Genesis("pet1", t0), e, tn); got != want {
			t.Fatalf("ordem alterou o resultado\n got: %+v\nwant: %+v", got, want)
		}
	}
}

// Reenviar o mesmo lote é o caminho feliz de um retry. Não pode mudar nada.
func TestFoldEhIdempotente(t *testing.T) {
	uma := Fold(Genesis("pet1", t0), evs(), tn)

	dobrado := append(evs(), evs()...)
	duas := Fold(Genesis("pet1", t0), dobrado, tn)

	if uma != duas {
		t.Fatalf("lote duplicado mudou o estado\n uma: %+v\nduas: %+v", uma, duas)
	}
}

// Fold incremental a partir de um snapshot deve bater com o replay do zero.
func TestFoldIncrementalBateComReplay(t *testing.T) {
	e := evs()

	completo := Fold(Genesis("pet1", t0), e, tn)

	parcial := Fold(Genesis("pet1", t0), e[:2], tn)
	incremental := Fold(parcial, e[2:], tn)

	if completo != incremental {
		t.Fatalf("snapshot divergiu do replay\n replay: %+v\n  incr: %+v", completo, incremental)
	}
}

// Caminho normal de um device offline: o servidor já projetou o bicho pra
// tela em T e salvou o snapshot, e só então chega um evento que aconteceu
// antes de T. Ele não pode sumir. Foi por causa deste caso que Fold e Project
// viraram funções separadas.
func TestEventoAtrasadoNaoPodeSumir(t *testing.T) {
	now := t0.Add(10 * time.Hour)
	atrasado := Event{ID: "01Z", At: now.Add(-3 * time.Minute), Kind: KindEffort, Kcal: 500, Zone: 1}

	snap := Fold(Genesis("pet1", t0), evs(), tn)
	_ = Project(snap, now, tn) // projetar não pode contaminar o snapshot
	incremental := Fold(snap, []Event{atrasado}, tn)

	replay := Fold(Genesis("pet1", t0), append(evs(), atrasado), tn)

	if incremental != replay {
		t.Fatalf("evento atrasado sumiu no caminho incremental\n incr: %+v\nreplay: %+v", incremental, replay)
	}
}

func TestCooldownDeEncontro(t *testing.T) {
	base := Genesis("pet1", t0)

	spam := make([]Event, 0, 20)
	for i := 0; i < 20; i++ {
		spam = append(spam, Event{
			ID:   string(rune('A' + i)),
			At:   t0.Add(time.Duration(i) * time.Minute),
			Kind: KindEncounter, PeerID: "p2",
		})
	}
	got := Fold(base, spam, tn)

	// só o primeiro encontro conta dentro da janela de cooldown
	um := Fold(base, spam[:1], tn)
	if got.Stats.Vinculo != um.Stats.Vinculo {
		t.Fatalf("cooldown não segurou: 20 encontros deram vínculo %d, 1 deu %d",
			got.Stats.Vinculo, um.Stats.Vinculo)
	}
}

func TestStatsNuncaEstouramOsLimites(t *testing.T) {
	cases := []struct {
		nome string
		ev   Event
	}{
		{"esforço absurdo", Event{ID: "x", At: t0.Add(time.Hour), Kind: KindEffort, Kcal: 65535, Zone: 5}},
		{"sono absurdo", Event{ID: "y", At: t0.Add(time.Hour), Kind: KindSleep, Minutes: 65535}},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			v := Project(Fold(Genesis("pet1", t0), []Event{c.ev}, tn), t0.Add(2*time.Hour), tn)
			for _, c := range []struct {
				nome string
				v    int32
			}{
				{"vigor", v.Stats.Vigor}, {"animo", v.Stats.Animo},
				{"saude", v.Stats.Saude}, {"vinculo", v.Stats.Vinculo},
			} {
				if c.v > Max || c.v < 0 {
					t.Errorf("%s fora dos limites: %d", c.nome, c.v)
				}
			}
		})
	}
}

func TestAbandonoLevaAoDecaimento(t *testing.T) {
	v := Project(Fold(Genesis("pet1", t0), nil, tn), t0.Add(30*24*time.Hour), tn)
	if v.Stats.Vigor != 0 || v.Stats.Animo != 0 {
		t.Fatalf("um mês largado deveria zerar vigor e ânimo, deu %+v", v.Stats)
	}
}

// Project é leitura. Chamar duas vezes com o mesmo now dá o mesmo resultado,
// e nunca altera o State de origem.
func TestProjectNaoMutaOEstado(t *testing.T) {
	now := t0.Add(10 * time.Hour)
	snap := Fold(Genesis("pet1", t0), evs(), tn)
	antes := snap

	a := Project(snap, now, tn)
	b := Project(snap, now, tn)

	if snap != antes {
		t.Fatalf("Project mutou o State de origem\n antes: %+v\ndepois: %+v", antes, snap)
	}
	if a != b {
		t.Fatalf("Project não é determinística\n a: %+v\n b: %+v", a, b)
	}
}

func TestMutacaoEhDeterministica(t *testing.T) {
	for i := 0; i < 50; i++ {
		if a, b := mutate(TraitNeutro, "seed-fixa"), mutate(TraitNeutro, "seed-fixa"); a != b {
			t.Fatalf("mutação não determinística: %v != %v", a, b)
		}
	}
}

// O service precisa saber se um evento que chegou seria aplicado sobre o
// snapshot ou descartado por ser velho demais, pra decidir se invalida o
// snapshot e refolda do genesis. Essa pergunta tem que ser respondida pelo
// próprio sim: reimplementar a comparação lá fora é garantir divergência no
// primeiro refactor daqui.
func TestWouldApplyConcordaComFold(t *testing.T) {
	s := Fold(Genesis("pet1", t0), evs(), tn)

	cases := []struct {
		nome string
		ev   Event
		want bool
	}{
		{"posterior ao último", Event{ID: "01Z", At: t0.Add(5 * time.Hour), Kind: KindInteract}, true},
		{"anterior ao último", Event{ID: "01Z", At: t0.Add(2 * time.Hour), Kind: KindInteract}, false},
		{"mesmo segundo, ID maior", Event{ID: "01E", At: t0.Add(4 * time.Hour), Kind: KindInteract}, true},
		{"mesmo segundo, ID menor", Event{ID: "01C", At: t0.Add(4 * time.Hour), Kind: KindInteract}, false},
		{"o próprio último evento", evs()[3], false},
	}

	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			if got := WouldApply(s, c.ev); got != c.want {
				t.Errorf("WouldApply = %v, esperado %v", got, c.want)
			}
			// a prova de verdade: WouldApply tem que concordar com o que o
			// Fold realmente faz, não com o que eu acho que ele faz
			mudou := Fold(s, []Event{c.ev}, tn) != s
			if mudou != c.want {
				t.Errorf("WouldApply disse %v mas Fold %s o evento",
					c.want, map[bool]string{true: "aplicou", false: "ignorou"}[mudou])
			}
		})
	}
}

// O nome do Kind no wire é contrato entre os três runtimes. Ele vive aqui e
// não na borda HTTP porque o device também serializa evento, e duas cópias da
// mesma tabela divergem no primeiro Kind novo: o servidor passa a rejeitar o
// que o device manda, e ninguém descobre até olhar o contador de rejeitados.
func TestNomeDoKindVaiEVolta(t *testing.T) {
	for k := KindEffort; k <= KindEncounter; k++ {
		nome := KindName(k)
		if nome == "" {
			t.Errorf("kind %d não tem nome no wire", k)
			continue
		}
		volta, ok := KindFromName(nome)
		if !ok || volta != k {
			t.Errorf("%d virou %q e voltou como %d (ok=%v)", k, nome, volta, ok)
		}
	}
}

// Os nomes são estáveis: mudar qualquer um quebra todo device já em campo,
// porque ele fala o nome antigo e o servidor deixa de entender.
func TestNomesDoKindSaoEstaveis(t *testing.T) {
	esperado := map[Kind]string{
		KindEffort:    "effort",
		KindSleep:     "sleep",
		KindInteract:  "interact",
		KindEncounter: "encounter",
	}
	for k, want := range esperado {
		if got := KindName(k); got != want {
			t.Errorf("KindName(%d) = %q, esperado %q: isso quebra device em campo", k, got, want)
		}
	}
}

func TestKindDesconhecidoNaoTemNome(t *testing.T) {
	if KindName(KindUnknown) != "" {
		t.Error("KindUnknown ganhou nome no wire")
	}
	if KindName(Kind(99)) != "" {
		t.Error("kind fora da faixa devolveu nome")
	}
	if _, ok := KindFromName("teleportou"); ok {
		t.Error("nome inventado foi aceito")
	}
}

// Pct é o que a tela e a API mostram. Nunca tinha sido testado direto, e é a
// única função do sim que faz conversão com perda.
func TestPctConverteAEscalaInteira(t *testing.T) {
	casos := []struct {
		centesimos int32
		want       uint8
	}{
		{0, 0},
		{1, 0},  // meio por cento não vira 1
		{99, 0}, // nem 0,99
		{100, 1},
		{5000, 50},
		{9999, 99}, // trunca, não arredonda: 99,99 é 99
		{Max, 100},
	}
	for _, c := range casos {
		if got := Pct(c.centesimos); got != c.want {
			t.Errorf("Pct(%d) = %d, esperado %d", c.centesimos, got, c.want)
		}
	}
}

// Pct só é seguro porque os atributos são clampados em [0, Max]. Se algum
// passasse de 25500, o uint8 daria a volta e um bicho no talo apareceria
// morrendo. Este teste é o que amarra as duas coisas.
func TestPctNaoDaAVoltaDentroDaFaixaValida(t *testing.T) {
	anterior := uint8(0)
	for v := int32(0); v <= Max; v += 37 {
		got := Pct(v)
		if got > 100 {
			t.Fatalf("Pct(%d) = %d, passou de 100", v, got)
		}
		if got < anterior {
			t.Fatalf("Pct(%d) = %d caiu em relação ao anterior (%d): deu a volta", v, got, anterior)
		}
		anterior = got
	}
}

// Cada estágio tem que ser alcançável, e a ordem tem que ser monotônica: o
// bicho não pula estágio nem regride ao crescer.
func TestTodoEstagioEhAlcancavelENaOrdem(t *testing.T) {
	// Limiares calibrados pro dono constante (3x/semana): ver stageFor.
	casos := []struct {
		growth uint32
		want   Stage
	}{
		{0, StageOvo}, {39, StageOvo},
		{40, StageFilhote}, {99, StageFilhote},
		{100, StageJovem}, {399, StageJovem},
		{400, StageAdulto}, {1499, StageAdulto},
		{1500, StageVeterano}, {1 << 20, StageVeterano},
	}
	for _, c := range casos {
		if got := stageFor(c.growth); got != c.want {
			t.Errorf("stageFor(%d) = %v, esperado %v", c.growth, got, c.want)
		}
	}

	anterior := StageOvo
	for g := uint32(0); g < 2000; g += 3 {
		s := stageFor(g)
		if s < anterior {
			t.Fatalf("growth %d regrediu de %v pra %v", g, anterior, s)
		}
		anterior = s
	}
}

// decai faz a conta em int64 e clampa nas duas pontas. O teto só é atingível
// com Tuning de regeneração (decaimento negativo), que ninguém usa hoje mas
// que o tipo permite: sem o clamp, isso estouraria o Max em silêncio.
func TestDecaiClampaNasDuasPontas(t *testing.T) {
	if got := decai(100, 1000, 1); got != 0 {
		t.Errorf("decaimento maior que o valor deu %d, esperado 0", got)
	}
	if got := decai(Max-10, 1000, -1000); got != Max {
		t.Errorf("decaimento negativo deu %d, esperado clamp em %d", got, Max)
	}
	// e não estoura int32 num abandono absurdo
	if got := decai(Max, 1<<40, 120); got != 0 {
		t.Errorf("abandono de %d horas deu %d, esperado 0 (overflow?)", int64(1)<<40, got)
	}
}

// Nome vazio e nome inventado são casos diferentes no código e ambos têm que
// recusar: um evento sem kind e um com kind desconhecido chegam pelo mesmo
// caminho na borda.
func TestKindFromNameRecusaVazioEInventado(t *testing.T) {
	for _, nome := range []string{"", "teleportou", "EFFORT", "effort "} {
		if k, ok := KindFromName(nome); ok {
			t.Errorf("KindFromName(%q) devolveu %v, esperado recusa", nome, k)
		}
	}
}

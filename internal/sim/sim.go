// Package sim é o núcleo da FERA.
//
// Invariante: este pacote é PURO. Sem I/O, sem time.Now(), sem rand global,
// sem iteração de map, sem ponto flutuante. Só stdlib mínima. Ele compila
// igual no servidor, no WASM do app e no ESP32 via TinyGo, e produz
// exatamente o mesmo resultado nos três.
package sim

import "time"

// SchemaVer muda sempre que uma regra altera o resultado de Fold.
// Mudou? Todos os snapshots viram lixo e são recalculados a partir do log.
//
// v2: Fold parou de embutir o decaimento até "agora" (isso virou Project) e
// o tempo no State virou unix segundos.
// v3: balanceamento calibrado contra personas reais, e growth passou a vir só
// de esforço e encontro. Ver internal/sim/balanceamento_test.go.
const SchemaVer uint8 = 3

// Atributos são inteiros em centésimos: 10000 == 100.00.
// Ponto flutuante está proibido aqui de propósito. Aritmética inteira é
// bit a bit idêntica no x86 do servidor, no ARM do celular e no Xtensa do
// ESP32. Float não te dá essa garantia de graça.
const Max int32 = 10000

type Kind uint8

const (
	KindUnknown Kind = iota
	KindEffort
	KindSleep
	KindInteract
	KindEncounter
)

// Event é imutável e identificado por um ULID gerado no cliente.
// Payload achatado de propósito: sem map, sem interface, sem json.
// Isso mantém o tipo comparável e amigável ao TinyGo.
type Event struct {
	ID   string
	At   time.Time
	Kind Kind

	Kcal    uint16 // KindEffort
	Zone    uint8  // KindEffort, 1..5
	Minutes uint16 // KindSleep
	PeerID  string // KindEncounter
}

// Nomes dos Kind no wire. Ficam no core, e não em cada borda, porque os três
// runtimes serializam evento: servidor, app e device. Duas cópias desta tabela
// divergiriam no primeiro Kind novo, e o sintoma seria o servidor rejeitando
// em silêncio o que o device manda.
//
// São ESTÁVEIS. O número do Kind é detalhe interno e pode ser reordenado; o
// nome é contrato com todo device já em campo.
var nomesKind = [...]string{
	KindEffort:    "effort",
	KindSleep:     "sleep",
	KindInteract:  "interact",
	KindEncounter: "encounter",
}

func KindName(k Kind) string {
	if int(k) >= len(nomesKind) {
		return ""
	}
	return nomesKind[k]
}

func KindFromName(nome string) (Kind, bool) {
	if nome == "" {
		return KindUnknown, false
	}
	for i, n := range nomesKind {
		if n == nome {
			return Kind(i), true
		}
	}
	return KindUnknown, false
}

// atSec é o ÚNICO lugar que converte o relógio da borda pro relógio interno.
// Ordenação, comparação de anterioridade e decaimento têm que usar a mesma
// granularidade: se divergirem, evento some no meio do caminho.
func atSec(ev Event) int64 { return ev.At.Unix() }

type Stage uint8

const (
	StageOvo Stage = iota
	StageFilhote
	StageJovem
	StageAdulto
	StageVeterano
)

type Trait uint8

const (
	TraitNeutro Trait = iota
	TraitTeimoso
	TraitAgitado
	TraitCalmo
	TraitFerino
)

type Stats struct {
	Vigor   int32
	Animo   int32
	Saude   int32
	Vinculo int32
}

// Pct converte pra 0..100 pra exibição. Só a UI usa isso.
func Pct(v int32) uint8 { return uint8(v / 100) }

// State é o fold puro dos eventos, e é isso que vira snapshot.
// Comparável com == de propósito: tempo é int64 unix, não time.Time, porque
// time.Time carrega ponteiro de location e relógio monotônico e sobrevive mal
// a um round-trip por JSONB ou por golden vector.
type State struct {
	PetID     string
	SchemaVer uint8
	Stats     Stats
	Stage     Stage
	Trait     Trait
	Growth    uint32

	// LastAtUnix é o instante do último EVENTO aplicado, nunca "agora".
	LastAtUnix int64
	LastID     string

	LastEncAtUnix int64

	// CarrySec guarda os segundos que ainda não completaram uma hora de
	// decaimento. É o que faz fold incremental dar EXATAMENTE o mesmo
	// resultado que replay do zero, sem erro de truncamento acumulado.
	CarrySec int32
}

// View é o estado como o dono vê AGORA: o fold dos eventos mais o decaimento
// do tempo parado desde o último evento.
//
// Não é persistível de propósito, e é um tipo separado justamente pra que
// ninguém consiga persistir. Se a projeção virasse snapshot, seu LastAtUnix
// seria "agora" e todo evento chegando depois com At anterior a esse instante
// seria descartado em silêncio, que é o caminho normal de um device offline.
type View struct {
	PetID  string
	Stats  Stats
	Stage  Stage
	Trait  Trait
	Growth uint32
}

// Tuning são todas as constantes de balanceamento, em centésimos por unidade.
// Nunca hardcode número mágico dentro da lógica. TODO: calibrar com uso real.
type Tuning struct {
	VigorPorKcal        int32 // centésimos por kcal
	SaudePorHoraSono    int32
	AnimoPorHoraSono    int32
	StrainPorZona       int32
	AnimoPorInteracao   int32
	VinculoPorInteracao int32
	VinculoPorEncontro  int32
	AnimoPorEncontro    int32

	DecaiVigorHora   int32
	DecaiAnimoHora   int32
	DecaiVinculoHora int32

	CooldownEncontro time.Duration
	GrowthPorEvento  uint32
}

// Calibrado, não chutado. Cada número abaixo existe porque uma persona de
// internal/sim/balanceamento_test.go se comporta como deve com ele, e deixa
// de se comportar sem ele. Mudar qualquer um quebra um teste que diz, em
// português, o que o bicho deveria fazer.
//
// As personas: sedentário, iniciante (2x/semana), constante (3x/semana),
// atleta (6x/semana), overtraining (6x no talo dormindo mal) e "sumiu".
func DefaultTuning() Tuning {
	return Tuning{
		// 500 kcal dão 30 pontos de vigor. Com o decaimento abaixo, 3x por
		// semana sustenta o vigor no talo e 2x por semana o mantém visível
		// mas sem encostar lá.
		VigorPorKcal:   6,
		DecaiVigorHora: 20, // 4,8 pontos/dia: quem para vê diferença em uma semana

		// Saúde é o BALANÇO entre carga e descanso, não um medidor de
		// atividade. A razão entre os dois é o que faz o atleta que dorme
		// bem se sustentar e o que dorme mal definhar, treinando quase igual.
		StrainPorZona:    400, // zona 5 custa 20 pontos
		SaudePorHoraSono: 240, // 8h repõem 19; 5h repõem 12

		// Ânimo responde a atenção: sono e botão. Sobe rápido e cai rápido.
		AnimoPorHoraSono:  100,
		AnimoPorInteracao: 600,
		AnimoPorEncontro:  300,
		DecaiAnimoHora:    40, // 9,6 pontos/dia

		// Vínculo é o atributo LENTO. Interagir todo dia rende +0,4 líquido
		// por dia, então ele mede consistência de meses, não um dia bom.
		VinculoPorInteracao: 400,
		VinculoPorEncontro:  900,
		DecaiVinculoHora:    15, // 3,6 pontos/dia

		CooldownEncontro: 6 * time.Hour,
		GrowthPorEvento:  10,
	}
}

func Genesis(petID string, at time.Time) State {
	return State{
		PetID:      petID,
		SchemaVer:  SchemaVer,
		Stats:      Stats{Vigor: 5000, Animo: 5000, Saude: 7000, Vinculo: 1000},
		Stage:      StageOvo,
		Trait:      TraitNeutro,
		LastAtUnix: at.Unix(),
	}
}

// Fold é a única forma de produzir estado persistível. Determinística,
// comutativa por ID e idempotente: aplicar o mesmo evento duas vezes não muda
// nada. Ela para no último evento e NÃO conhece "agora": quem quer o
// decaimento do tempo parado chama Project.
func Fold(s State, evs []Event, t Tuning) State {
	ordered := make([]Event, len(evs))
	copy(ordered, evs)
	sortEvents(ordered)

	var prevID string
	for _, ev := range ordered {
		if ev.ID == prevID { // duplicata dentro do próprio lote
			continue
		}
		prevID = ev.ID
		if !isAfter(ev, s) { // já aplicado ou anterior ao snapshot
			continue
		}
		at := atSec(ev)
		s = decay(s, at, t)
		s = apply(s, ev, t)
		s.LastAtUnix = at
		s.LastID = ev.ID
	}

	s.Stage = stageFor(s.Growth)
	s.SchemaVer = SchemaVer
	return s
}

// Project aplica o decaimento do tempo parado entre o último evento e now.
// É o que a tela mostra. O resultado não volta pro Fold nem pro banco.
func Project(s State, now time.Time, t Tuning) View {
	s = decay(s, now.Unix(), t)
	return View{
		PetID:  s.PetID,
		Stats:  s.Stats,
		Stage:  stageFor(s.Growth),
		Trait:  s.Trait,
		Growth: s.Growth,
	}
}

// sortEvents é insertion sort estável por (segundo, ID). Escrito à mão porque
// sort.SliceStable puxa reflect.Swapper, e reflect infla o binário no TinyGo.
// O lote tem no máximo 200 eventos (limite do api-contract), então O(n²) aqui
// é mais barato que a alternativa.
func sortEvents(evs []Event) {
	for i := 1; i < len(evs); i++ {
		ev := evs[i]
		j := i - 1
		for j >= 0 && menor(ev, evs[j]) {
			evs[j+1] = evs[j]
			j--
		}
		evs[j+1] = ev
	}
}

func menor(a, b Event) bool {
	sa, sb := atSec(a), atSec(b)
	if sa == sb {
		return a.ID < b.ID
	}
	return sa < sb
}

// WouldApply responde se ev entraria no Fold sobre s, ou seria descartado por
// ser anterior ao último evento já aplicado.
//
// Existe porque quem persiste snapshot precisa dessa resposta pra decidir
// entre fold incremental e replay do genesis, e essa decisão não pode ser
// tomada com uma cópia da regra: tem que ser a MESMA que o Fold usa.
func WouldApply(s State, ev Event) bool { return isAfter(ev, s) }

func isAfter(ev Event, s State) bool {
	at := atSec(ev)
	if at > s.LastAtUnix {
		return true
	}
	if at == s.LastAtUnix {
		return ev.ID > s.LastID
	}
	return false
}

func decay(s State, toSec int64, t Tuning) State {
	if toSec <= s.LastAtUnix {
		return s
	}
	total := toSec - s.LastAtUnix + int64(s.CarrySec)
	hours := total / 3600
	s.CarrySec = int32(total % 3600)

	if hours > 0 {
		s.Stats.Vigor = decai(s.Stats.Vigor, hours, t.DecaiVigorHora)
		s.Stats.Animo = decai(s.Stats.Animo, hours, t.DecaiAnimoHora)
		s.Stats.Vinculo = decai(s.Stats.Vinculo, hours, t.DecaiVinculoHora)
	}
	s.LastAtUnix = toSec
	return s
}

// decai faz a conta em int64 porque hours * porHora estoura int32 num
// abandono longo, e um overflow aqui vira ganho de atributo em vez de perda.
func decai(v int32, hours int64, porHora int32) int32 {
	r := int64(v) - hours*int64(porHora)
	if r < 0 {
		return 0
	}
	if r > int64(Max) {
		return Max
	}
	return int32(r)
}

func apply(s State, ev Event, t Tuning) State {
	switch ev.Kind {
	case KindEffort:
		s.Stats.Vigor = clamp(s.Stats.Vigor + int32(ev.Kcal)*t.VigorPorKcal)
		s.Stats.Saude = clamp(s.Stats.Saude - int32(ev.Zone)*t.StrainPorZona)
		s.Growth += t.GrowthPorEvento

	case KindSleep:
		// Sono NÃO faz crescer. A primeira linha do README é "só cresce com
		// esforço físico real", e antes desta calibragem um sedentário que
		// registrava sono e apertava o botão chegava a "jovem" em 57 dias
		// sem treinar um único dia.
		min := int32(ev.Minutes)
		s.Stats.Saude = clamp(s.Stats.Saude + min*t.SaudePorHoraSono/60)
		s.Stats.Animo = clamp(s.Stats.Animo + min*t.AnimoPorHoraSono/60)

	case KindInteract:
		// Botão também não faz crescer: "não dá pra alimentar apertando botão".
		s.Stats.Animo = clamp(s.Stats.Animo + t.AnimoPorInteracao)
		s.Stats.Vinculo = clamp(s.Stats.Vinculo + t.VinculoPorInteracao)

	case KindEncounter:
		// O cooldown vive AQUI, não no servidor. Se ficasse no servidor,
		// dois devices offline lado a lado farmariam vínculo infinito.
		if atSec(ev)-s.LastEncAtUnix < int64(t.CooldownEncontro/time.Second) {
			return s
		}
		s.Stats.Vinculo = clamp(s.Stats.Vinculo + t.VinculoPorEncontro)
		s.Stats.Animo = clamp(s.Stats.Animo + t.AnimoPorEncontro)
		s.LastEncAtUnix = atSec(ev)
		s.Growth += t.GrowthPorEvento
		s.Trait = mutate(s.Trait, ev.ID+s.PetID)
	}
	return s
}

// mutate usa seed derivado do ID do evento. Device e servidor chegam no
// mesmo traço sem trocar uma única mensagem.
//
// Nunca devolve TraitNeutro (é só o estado de nascimento) e pode devolver o
// traço atual. As duas coisas são intencionais.
func mutate(cur Trait, seed string) Trait {
	r := splitmix64(fnv64(seed))
	if r%100 < 75 { // 25% de chance de mutar
		return cur
	}
	// Bits altos pra escolher, bits baixos pra decidir. Usar r%4 aqui
	// correlacionaria com o r%100 de cima e enviesaria a distribuição.
	return Trait(1 + (r>>32)%4)
}

// Limiares calibrados pro dono constante (3x/semana, ~13 esforços/mês), que
// é a persona de referência. Cada esforço vale GrowthPorEvento:
//
//	filhote   4 esforços   ~1 semana
//	jovem    10 esforços   ~3 semanas
//	adulto   40 esforços   ~3 meses
//	veterano 150 esforços  ~1 ano
//
// Travado por TestRitmoDeEvolucaoDoDonoConstante.
func stageFor(growth uint32) Stage {
	switch {
	case growth >= 1500:
		return StageVeterano
	case growth >= 400:
		return StageAdulto
	case growth >= 100:
		return StageJovem
	case growth >= 40:
		return StageFilhote
	default:
		return StageOvo
	}
}

func clamp(v int32) int32 {
	if v > Max {
		return Max
	}
	if v < 0 {
		return 0
	}
	return v
}

func fnv64(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func splitmix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

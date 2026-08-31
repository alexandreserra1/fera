package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ale/fera/internal/sim"
)

var (
	t0  = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	pet = "11111111-1111-1111-1111-111111111111"
)

func relogio(t time.Time) func() time.Time { return func() time.Time { return t } }

func lote(n int) []sim.Event {
	evs := make([]sim.Event, 0, n)
	for i := range n {
		evs = append(evs, sim.Event{
			ID: string(rune('A'+i/26)) + string(rune('A'+i%26)),
			At: t0.Add(time.Duration(i) * time.Minute), Kind: sim.KindInteract,
		})
	}
	return evs
}

func TestGetSemNenhumEventoDevolveOGenesis(t *testing.T) {
	svc := New(novoFakeEvents(), novoFakeSnapshots(), relogio(t0), sim.DefaultTuning())

	v, err := svc.Get(context.Background(), pet)
	if err != nil {
		t.Fatal(err)
	}
	want := sim.Project(sim.Genesis(pet, t0), t0, sim.DefaultTuning())
	if v != want {
		t.Errorf("got %+v, want %+v", v, want)
	}
}

func TestGetSemSnapshotRefoldaDoGenesis(t *testing.T) {
	ev := novoFakeEvents(lote(4)...)
	snaps := novoFakeSnapshots()
	agora := t0.Add(10 * time.Hour)
	svc := New(ev, snaps, relogio(agora), sim.DefaultTuning())

	v, err := svc.Get(context.Background(), pet)
	if err != nil {
		t.Fatal(err)
	}

	want := sim.Project(sim.Fold(sim.Genesis(pet, t0), lote(4), sim.DefaultTuning()), agora, sim.DefaultTuning())
	if v != want {
		t.Errorf("replay do genesis divergiu\n got: %+v\nwant: %+v", v, want)
	}
	// e o snapshot tem que ter sido gravado, senão o próximo Get refolda tudo de novo
	if snaps.vezes("Save") != 1 {
		t.Errorf("Save chamado %d vezes, esperado 1", snaps.vezes("Save"))
	}
}

func TestGetComCacheQuenteNaoTocaNoStore(t *testing.T) {
	ev := novoFakeEvents(lote(4)...)
	snaps := novoFakeSnapshots()
	svc := New(ev, snaps, relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())
	ctx := context.Background()

	if _, err := svc.Get(ctx, pet); err != nil {
		t.Fatal(err)
	}
	loadsDepoisDoPrimeiro := snaps.vezes("Load")
	sinceDepoisDoPrimeiro := ev.vezes("Since")

	for range 20 {
		if _, err := svc.Get(ctx, pet); err != nil {
			t.Fatal(err)
		}
	}

	if snaps.vezes("Load") != loadsDepoisDoPrimeiro {
		t.Errorf("cache quente foi ao snapshot store %d vezes", snaps.vezes("Load")-loadsDepoisDoPrimeiro)
	}
	if ev.vezes("Since") != sinceDepoisDoPrimeiro {
		t.Errorf("cache quente foi ao event store %d vezes", ev.vezes("Since")-sinceDepoisDoPrimeiro)
	}
}

// Cache guarda State, não View. Se guardasse View, o bicho congelaria no
// instante do fold e o decaimento pararia de andar durante o TTL inteiro.
func TestCacheQuenteAindaDecaiComOTempo(t *testing.T) {
	ev := novoFakeEvents(lote(4)...)
	agora := t0.Add(1 * time.Hour)
	svc := New(ev, novoFakeSnapshots(), func() time.Time { return agora }, sim.DefaultTuning())
	ctx := context.Background()

	antes, err := svc.Get(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}

	agora = t0.Add(48 * time.Hour) // dois dias depois, mesmo cache
	depois, err := svc.Get(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}

	if depois.Stats.Animo >= antes.Stats.Animo {
		t.Errorf("ânimo não decaiu com o cache quente: %d -> %d",
			antes.Stats.Animo, depois.Stats.Animo)
	}
}

func TestGetComSnapshotFoldaSoODelta(t *testing.T) {
	todos := lote(6)
	ev := novoFakeEvents(todos...)
	snaps := novoFakeSnapshots()

	// snapshot já cobre os 4 primeiros
	parcial := sim.Fold(sim.Genesis(pet, t0), todos[:4], sim.DefaultTuning())
	_ = snaps.Save(context.Background(), pet, parcial, 4)
	snaps.chamou = map[string]int{}

	agora := t0.Add(10 * time.Hour)
	svc := New(ev, snaps, relogio(agora), sim.DefaultTuning())

	v, err := svc.Get(context.Background(), pet)
	if err != nil {
		t.Fatal(err)
	}
	want := sim.Project(sim.Fold(sim.Genesis(pet, t0), todos, sim.DefaultTuning()), agora, sim.DefaultTuning())
	if v != want {
		t.Errorf("fold incremental divergiu do replay\n got: %+v\nwant: %+v", v, want)
	}
	// não pode ter chamado FirstAt: só o replay do genesis precisa disso
	if ev.vezes("FirstAt") != 0 {
		t.Errorf("foi buscar o genesis mesmo tendo snapshot")
	}
}

// Cem devices pedindo o mesmo pet frio não podem virar cem folds.
func TestStampedeVira1FoldSo(t *testing.T) {
	ev := novoFakeEvents(lote(20)...)
	snaps := novoFakeSnapshots()
	svc := New(ev, snaps, relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())

	const n = 100
	var wg sync.WaitGroup
	vistas := make([]sim.View, n)
	erros := make([]error, n)
	largada := make(chan struct{})

	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			<-largada
			vistas[i], erros[i] = svc.Get(context.Background(), pet)
		}()
	}
	close(largada)
	wg.Wait()

	for i := range n {
		if erros[i] != nil {
			t.Fatalf("goroutine %d: %v", i, erros[i])
		}
		if vistas[i] != vistas[0] {
			t.Fatalf("goroutine %d viu estado diferente da 0", i)
		}
	}
	if got := snaps.vezes("Load"); got != 1 {
		t.Errorf("%d goroutines geraram %d folds, esperado 1", n, got)
	}
}

func TestIngestContaAceitosEDuplicados(t *testing.T) {
	svc := New(novoFakeEvents(), novoFakeSnapshots(), relogio(t0), sim.DefaultTuning())
	ctx := context.Background()

	r1, err := svc.Ingest(ctx, pet, lote(10))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Accepted != 10 || r1.Duplicates != 0 {
		t.Errorf("primeiro lote: %+v, esperado 10 aceitos e 0 duplicados", r1)
	}

	r2, err := svc.Ingest(ctx, pet, lote(10))
	if err != nil {
		t.Fatalf("reenvio devolveu erro: %v", err)
	}
	if r2.Accepted != 0 || r2.Duplicates != 10 {
		t.Errorf("reenvio: %+v, esperado 0 aceitos e 10 duplicados", r2)
	}
	if r2.Cursor != r1.Cursor {
		t.Errorf("cursor mudou no reenvio: %d -> %d", r1.Cursor, r2.Cursor)
	}
}

func TestIngestInvalidaOCache(t *testing.T) {
	ev := novoFakeEvents(lote(4)...)
	svc := New(ev, novoFakeSnapshots(), relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())
	ctx := context.Background()

	antes, err := svc.Get(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}

	novo := sim.Event{ID: "ZZ", At: t0.Add(5 * time.Hour), Kind: sim.KindEffort, Kcal: 800, Zone: 2}
	if _, err := svc.Ingest(ctx, pet, []sim.Event{novo}); err != nil {
		t.Fatal(err)
	}

	depois, err := svc.Get(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	if depois == antes {
		t.Error("Get devolveu o estado velho: o Ingest não invalidou o cache")
	}
	if depois.Stats.Vigor <= antes.Stats.Vigor {
		t.Errorf("o esforço novo não entrou: vigor %d -> %d", antes.Stats.Vigor, depois.Stats.Vigor)
	}
}

// A regra escrita no docs/03: evento anterior ao último já foldado não pode
// ser aplicado incrementalmente, porque o Fold o descartaria. O snapshot tem
// que ser jogado fora pra que o próximo Get refolde do genesis.
func TestIngestDeEventoAtrasadoInvalidaOSnapshot(t *testing.T) {
	todos := lote(6)
	ev := novoFakeEvents(todos...)
	snaps := novoFakeSnapshots()
	agora := t0.Add(10 * time.Hour)
	svc := New(ev, snaps, relogio(agora), sim.DefaultTuning())
	ctx := context.Background()

	if _, err := svc.Get(ctx, pet); err != nil { // materializa o snapshot
		t.Fatal(err)
	}
	if !snaps.tem {
		t.Fatal("snapshot não foi materializado")
	}

	// aconteceu ANTES de tudo que já foi foldado
	atrasado := sim.Event{ID: "AAA", At: t0.Add(-2 * time.Hour), Kind: sim.KindEffort, Kcal: 900, Zone: 1}
	if _, err := svc.Ingest(ctx, pet, []sim.Event{atrasado}); err != nil {
		t.Fatal(err)
	}
	if snaps.vezes("Delete") != 1 {
		t.Errorf("snapshot não foi invalidado: Delete chamado %d vezes", snaps.vezes("Delete"))
	}

	// e o replay tem que enxergar o evento atrasado
	v, err := svc.Get(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	want := sim.Project(
		sim.Fold(sim.Genesis(pet, atrasado.At), append([]sim.Event{atrasado}, todos...), sim.DefaultTuning()),
		agora, sim.DefaultTuning())
	if v != want {
		t.Errorf("evento atrasado sumiu no replay\n got: %+v\nwant: %+v", v, want)
	}
}

// Evento novo (posterior ao snapshot) NÃO pode custar um replay: seria jogar
// fora o snapshot em todo ingest normal, que é o caminho de 99% dos casos.
func TestIngestNormalNaoInvalidaOSnapshot(t *testing.T) {
	ev := novoFakeEvents(lote(4)...)
	snaps := novoFakeSnapshots()
	svc := New(ev, snaps, relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())
	ctx := context.Background()

	if _, err := svc.Get(ctx, pet); err != nil {
		t.Fatal(err)
	}
	novo := sim.Event{ID: "ZZ", At: t0.Add(5 * time.Hour), Kind: sim.KindInteract}
	if _, err := svc.Ingest(ctx, pet, []sim.Event{novo}); err != nil {
		t.Fatal(err)
	}
	if snaps.vezes("Delete") != 0 {
		t.Errorf("ingest normal jogou o snapshot fora %d vezes", snaps.vezes("Delete"))
	}
}

// Log maior que uma página tem que ser foldado inteiro, não só a primeira.
func TestReplayAtravessaVariasPaginas(t *testing.T) {
	n := pageSize*2 + 7
	todos := lote(n)
	ev := novoFakeEvents(todos...)
	agora := t0.Add(time.Duration(n) * time.Minute).Add(time.Hour)
	svc := New(ev, novoFakeSnapshots(), relogio(agora), sim.DefaultTuning())

	v, err := svc.Get(context.Background(), pet)
	if err != nil {
		t.Fatal(err)
	}
	want := sim.Project(sim.Fold(sim.Genesis(pet, t0), todos, sim.DefaultTuning()), agora, sim.DefaultTuning())
	if v != want {
		t.Errorf("replay parou antes do fim do log\n got: %+v\nwant: %+v", v, want)
	}
}

// Reenvio de lote é o caminho FELIZ de um retry e tem que ser barato. Se a
// reconciliação olhar os duplicados, ela conclui (corretamente) que eles não
// aplicariam sobre o snapshot, joga o snapshot fora, e todo retry passa a
// custar um replay do log inteiro. Só evento NOVO pode disparar invalidação.
func TestReenvioDeLoteNaoInvalidaOSnapshot(t *testing.T) {
	todos := lote(6)
	ev := novoFakeEvents(todos...)
	snaps := novoFakeSnapshots()
	svc := New(ev, snaps, relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())
	ctx := context.Background()

	if _, err := svc.Get(ctx, pet); err != nil { // materializa o snapshot
		t.Fatal(err)
	}
	if !snaps.tem {
		t.Fatal("snapshot não foi materializado")
	}

	r, err := svc.Ingest(ctx, pet, todos) // o MESMO lote de novo
	if err != nil {
		t.Fatal(err)
	}
	if r.Accepted != 0 || r.Duplicates != len(todos) {
		t.Fatalf("reenvio: %+v, esperado 0 aceitos e %d duplicados", r, len(todos))
	}
	if snaps.vezes("Delete") != 0 {
		t.Errorf("reenvio de lote duplicado jogou o snapshot fora")
	}
	if !snaps.tem {
		t.Error("snapshot sumiu depois de um reenvio")
	}
}

// Lote misto: um duplicado e um novo-porém-atrasado. O novo tem que disparar
// a invalidação; o duplicado, sozinho, não teria.
func TestLoteMistoInvalidaSoPeloEventoNovo(t *testing.T) {
	todos := lote(6)
	ev := novoFakeEvents(todos...)
	snaps := novoFakeSnapshots()
	svc := New(ev, snaps, relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())
	ctx := context.Background()

	if _, err := svc.Get(ctx, pet); err != nil {
		t.Fatal(err)
	}

	atrasado := sim.Event{ID: "NOVO", At: t0.Add(-time.Hour), Kind: sim.KindInteract}
	if _, err := svc.Ingest(ctx, pet, []sim.Event{todos[0], atrasado}); err != nil {
		t.Fatal(err)
	}
	if snaps.vezes("Delete") != 1 {
		t.Errorf("Delete chamado %d vezes, esperado 1", snaps.vezes("Delete"))
	}
}

// O caso de produção que o teste de reenvio puro não alcança: o device manda o
// que não teve ACK (duplicado, e velho em relação ao snapshot) JUNTO com o que
// gerou desde então (novo e mais recente). O duplicado, sozinho, faria
// WouldApply dizer não. Se a reconciliação olhasse o lote inteiro em vez de só
// os novos, esse lote misto jogaria o snapshot fora sem nenhum motivo.
func TestDuplicadoVelhoJuntoComNovoRecenteNaoInvalida(t *testing.T) {
	todos := lote(6)
	ev := novoFakeEvents(todos...)
	snaps := novoFakeSnapshots()
	svc := New(ev, snaps, relogio(t0.Add(10*time.Hour)), sim.DefaultTuning())
	ctx := context.Background()

	if _, err := svc.Get(ctx, pet); err != nil { // snapshot cobre os 6
		t.Fatal(err)
	}

	// todos[0] é duplicado E anterior ao último foldado; recente é novo e posterior
	recente := sim.Event{ID: "RECENTE", At: t0.Add(9 * time.Hour), Kind: sim.KindInteract}
	r, err := svc.Ingest(ctx, pet, []sim.Event{todos[0], recente})
	if err != nil {
		t.Fatal(err)
	}
	if r.Accepted != 1 || r.Duplicates != 1 {
		t.Fatalf("%+v, esperado 1 aceito e 1 duplicado", r)
	}
	if snaps.vezes("Delete") != 0 {
		t.Errorf("o duplicado velho disparou invalidação: Delete chamado %d vezes",
			snaps.vezes("Delete"))
	}
}

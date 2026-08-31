package repo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ale/fera/internal/repo"
	"github.com/ale/fera/internal/sim"
)

var t0 = time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)

// pet novo por teste: sem isso os testes se contaminam pela ordem de execução
func novoPet(t *testing.T) string {
	t.Helper()
	return uuid.NewString()
}

func lote(n int) []sim.Event {
	evs := make([]sim.Event, 0, n)
	for i := 0; i < n; i++ {
		evs = append(evs, sim.Event{
			ID:   fmt.Sprintf("01J%03d", i),
			At:   t0.Add(time.Duration(i) * time.Minute),
			Kind: sim.KindEffort,
			Kcal: uint16(100 + i),
			Zone: uint8(1 + i%5),
		})
	}
	return evs
}

func TestAppendInsereOLoteInteiro(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	novos, cursor, err := r.Append(ctx, pet, lote(20))
	if err != nil {
		t.Fatal(err)
	}
	if len(novos) != 20 {
		t.Errorf("aceitos = %d, esperado 20", len(novos))
	}
	if cursor <= 0 {
		t.Errorf("cursor = %d, esperado > 0", cursor)
	}
}

// Reenviar o lote é o caminho FELIZ de um retry, não um erro. Tem que ser
// no-op silencioso, e o cursor tem que continuar sendo devolvido.
func TestAppendReenvioEhNoOp(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	_, cursor1, err := r.Append(ctx, pet, lote(20))
	if err != nil {
		t.Fatal(err)
	}

	novos, cursor2, err := r.Append(ctx, pet, lote(20))
	if err != nil {
		t.Fatalf("reenvio devolveu erro: %v", err)
	}
	if len(novos) != 0 {
		t.Errorf("reenvio inseriu %d eventos, esperado 0", len(novos))
	}
	if cursor2 != cursor1 {
		t.Errorf("cursor mudou no reenvio: %d -> %d", cursor1, cursor2)
	}

	evs, _, err := r.Since(ctx, pet, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 20 {
		t.Errorf("banco tem %d eventos depois de 2 lotes iguais, esperado 20", len(evs))
	}
}

// Lote que mistura conhecido e novo é o caso real: o device reenvia o que não
// teve ACK junto com o que gerou desde então.
func TestAppendLoteParcialmenteNovo(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	if _, _, err := r.Append(ctx, pet, lote(10)); err != nil {
		t.Fatal(err)
	}
	novos, _, err := r.Append(ctx, pet, lote(25))
	if err != nil {
		t.Fatal(err)
	}
	if len(novos) != 15 {
		t.Errorf("aceitos = %d, esperado 15 (25 enviados, 10 já existiam)", len(novos))
	}
}

func TestSinceDevolveEmOrdemDeSeq(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	if _, _, err := r.Append(ctx, pet, lote(20)); err != nil {
		t.Fatal(err)
	}

	evs, _, err := r.Since(ctx, pet, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 {
		t.Fatalf("limit não respeitado: %d eventos", len(evs))
	}
	for i := 1; i < len(evs); i++ {
		if !evs[i].At.After(evs[i-1].At) {
			t.Errorf("fora de ordem em %d: %v depois de %v", i, evs[i].At, evs[i-1].At)
		}
	}

	// o payload tem que sobreviver ao round-trip pelo JSONB
	if evs[0].ID != "01J000" || evs[0].Kind != sim.KindEffort || evs[0].Kcal != 100 || evs[0].Zone != 1 {
		t.Errorf("payload não sobreviveu ao round-trip: %+v", evs[0])
	}
	if !evs[0].At.Equal(t0) {
		t.Errorf("occurred_at não sobreviveu: %v, esperado %v", evs[0].At, t0)
	}
}

func TestSincePaginaSemPularNemRepetir(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	if _, _, err := r.Append(ctx, pet, lote(20)); err != nil {
		t.Fatal(err)
	}

	vistos := map[string]bool{}
	var cursor int64
	for range 10 {
		pagina, prox, err := r.Since(ctx, pet, cursor, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(pagina) == 0 {
			if prox != cursor {
				t.Errorf("página vazia mexeu no cursor: %d -> %d", cursor, prox)
			}
			break
		}
		if prox <= cursor {
			t.Fatalf("cursor não avançou: %d -> %d", cursor, prox)
		}
		for _, ev := range pagina {
			if vistos[ev.ID] {
				t.Fatalf("evento %s veio duas vezes", ev.ID)
			}
			vistos[ev.ID] = true
		}
		cursor = prox
	}
	if len(vistos) != 20 {
		t.Errorf("paginação viu %d eventos, esperado 20", len(vistos))
	}
}

func TestSinceNaoVazaEventoDeOutroPet(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	a, b := novoPet(t), novoPet(t)

	if _, _, err := r.Append(ctx, a, lote(5)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Append(ctx, b, lote(5)); err != nil {
		t.Fatal(err)
	}

	evs, _, err := r.Since(ctx, a, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 {
		t.Errorf("pet A enxergou %d eventos, esperado 5: vazou do pet B", len(evs))
	}
}

// O caso que realmente quebra em produção: dois devices do mesmo dono, ou um
// device com retry agressivo, empurrando o MESMO lote ao mesmo tempo. Se a
// idempotência dependesse de um SELECT antes do INSERT, isto duplicaria.
func TestAppendConcorrenteNaoDuplica(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	const n = 8
	var wg sync.WaitGroup
	aceitos := make([]int, n)
	erros := make([]error, n)

	wg.Add(n)
	for i := range n {
		go func() {
			defer wg.Done()
			n, _, e := r.Append(ctx, pet, lote(20))
			aceitos[i], erros[i] = len(n), e
		}()
	}
	wg.Wait()

	total := 0
	for i := range n {
		if erros[i] != nil {
			t.Errorf("goroutine %d falhou: %v", i, erros[i])
		}
		total += aceitos[i]
	}
	if total != 20 {
		t.Errorf("soma dos aceitos = %d, esperado exatamente 20", total)
	}

	evs, _, err := r.Since(ctx, pet, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 20 {
		t.Errorf("banco tem %d eventos, esperado 20", len(evs))
	}
}

func TestAppendLoteVazio(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	novos, cursor, err := r.Append(ctx, pet, nil)
	if err != nil {
		t.Fatalf("lote vazio devolveu erro: %v", err)
	}
	if len(novos) != 0 || cursor != 0 {
		t.Errorf("lote vazio em pet novo: aceitos=%d cursor=%d, esperado 0 e 0", len(novos), cursor)
	}
}

// Duplicata DENTRO do próprio lote, não entre lotes. O device pode empurrar o
// mesmo ULID duas vezes no mesmo POST, e ON CONFLICT resolvendo conflito
// contra linha que a própria statement acabou de inserir não é comportamento
// que se deva assumir sem provar.
func TestAppendLoteComDuplicataInterna(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	l := lote(10)
	l = append(l, l...) // 20 itens, 10 ULIDs distintos

	novos, _, err := r.Append(ctx, pet, l)
	if err != nil {
		t.Fatalf("lote com duplicata interna devolveu erro: %v", err)
	}
	if len(novos) != 10 {
		t.Errorf("aceitos = %d, esperado 10 (20 enviados, 10 ULIDs distintos)", len(novos))
	}

	evs, _, err := r.Since(ctx, pet, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 10 {
		t.Errorf("banco tem %d linhas, esperado 10", len(evs))
	}
}

// O sim é a única autoridade sobre o estado, então o repo precisa devolver
// eventos que o Fold aceite sem perder nada no caminho de ida e volta.
func TestRoundTripAlimentaOFoldSemPerda(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	original := []sim.Event{
		{ID: "01JA", At: t0.Add(1 * time.Hour), Kind: sim.KindEffort, Kcal: 500, Zone: 3},
		{ID: "01JB", At: t0.Add(2 * time.Hour), Kind: sim.KindInteract},
		{ID: "01JC", At: t0.Add(3 * time.Hour), Kind: sim.KindSleep, Minutes: 420},
		{ID: "01JD", At: t0.Add(4 * time.Hour), Kind: sim.KindEncounter, PeerID: "p2"},
	}
	if _, _, err := r.Append(ctx, pet, original); err != nil {
		t.Fatal(err)
	}

	volta, _, err := r.Since(ctx, pet, 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	tn := sim.DefaultTuning()
	want := sim.Fold(sim.Genesis(pet, t0), original, tn)
	got := sim.Fold(sim.Genesis(pet, t0), volta, tn)
	if got != want {
		t.Fatalf("estado divergiu depois do round-trip pelo banco\n got: %+v\nwant: %+v", got, want)
	}
}

// O cursor devolvido pelo Since é o que vira folded_seq no snapshot. Ele tem
// que ser o seq da ÚLTIMA linha da página, não o maior seq do pet: senão o
// service marcaria como foldado um evento que ainda não leu.
func TestSinceDevolveOSeqDaUltimaLinhaDaPagina(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	if _, _, err := r.Append(ctx, pet, lote(20)); err != nil {
		t.Fatal(err)
	}

	primeira, cursor1, err := r.Since(ctx, pet, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(primeira) != 5 {
		t.Fatalf("página com %d eventos, esperado 5", len(primeira))
	}

	_, cursorTudo, err := r.Since(ctx, pet, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if cursor1 >= cursorTudo {
		t.Errorf("cursor da 1a página (%d) devia ser menor que o do lote inteiro (%d)",
			cursor1, cursorTudo)
	}

	// continuar do cursor1 tem que trazer exatamente o que faltou
	resto, _, err := r.Since(ctx, pet, cursor1, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(resto) != 15 {
		t.Errorf("resto tem %d eventos, esperado 15", len(resto))
	}
	if resto[0].ID != "01J005" {
		t.Errorf("resto começa em %s, esperado 01J005", resto[0].ID)
	}
}

// FirstAt é "quando esse bicho nasceu": o evento cronologicamente mais antigo.
// Cronologicamente, NÃO por seq. seq é ordem de inserção, e um device offline
// empurra passado. Ancorar o genesis no primeiro inserido faria o Fold
// descartar tudo que aconteceu antes dele.
func TestFirstAtEhCronologicoNaoPorSeq(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	// inserido primeiro, mas aconteceu DEPOIS
	tarde := sim.Event{ID: "01JB", At: t0.Add(5 * time.Hour), Kind: sim.KindInteract}
	cedo := sim.Event{ID: "01JA", At: t0.Add(1 * time.Hour), Kind: sim.KindInteract}

	if _, _, err := r.Append(ctx, pet, []sim.Event{tarde}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Append(ctx, pet, []sim.Event{cedo}); err != nil {
		t.Fatal(err)
	}

	at, ok, err := r.FirstAt(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("pet com eventos devolveu ok=false")
	}
	if !at.Equal(cedo.At) {
		t.Errorf("FirstAt = %v, esperado %v (pegou o primeiro por seq, não por tempo)", at, cedo.At)
	}
}

func TestFirstAtDePetSemEvento(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)

	_, ok, err := r.FirstAt(ctx, novoPet(t))
	if err != nil {
		t.Fatalf("pet sem evento devolveu erro: %v", err)
	}
	if ok {
		t.Error("pet sem evento devolveu ok=true")
	}
}

// Append devolve QUAIS eventos entraram, não só quantos. O service usa essa
// lista pra separar "evento novo e atrasado", que obriga replay do snapshot,
// de "evento duplicado", que já está foldado e não obriga nada.
func TestAppendDevolveQuaisEntraram(t *testing.T) {
	ctx := context.Background()
	r := repo.NewEventRepo(pool)
	pet := novoPet(t)

	if _, _, err := r.Append(ctx, pet, lote(3)); err != nil {
		t.Fatal(err)
	}

	novos, _, err := r.Append(ctx, pet, lote(5))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, id := range novos {
		got[id] = true
	}
	want := map[string]bool{"01J003": true, "01J004": true}
	if len(got) != len(want) {
		t.Fatalf("entraram %v, esperado %v", novos, []string{"01J003", "01J004"})
	}
	for id := range want {
		if !got[id] {
			t.Errorf("%s devia ter entrado, veio %v", id, novos)
		}
	}
}

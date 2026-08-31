package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/ale/fera/internal/repo"
	"github.com/ale/fera/internal/sim"
)

func TestSnapshotAusenteNaoEhErro(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)

	_, _, ok, err := r.Load(ctx, novoPet(t))
	if err != nil {
		t.Fatalf("pet sem snapshot devolveu erro: %v", err)
	}
	if ok {
		t.Error("pet sem snapshot devolveu ok=true")
	}
}

func TestSnapshotSobreviveAoRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)
	pet := novoPet(t)

	want := sim.Fold(sim.Genesis(pet, t0), []sim.Event{
		{ID: "01JA", At: t0.Add(time.Hour), Kind: sim.KindEffort, Kcal: 500, Zone: 3},
		{ID: "01JB", At: t0.Add(2 * time.Hour), Kind: sim.KindEncounter, PeerID: "p2"},
	}, sim.DefaultTuning())

	if err := r.Save(ctx, pet, want, 42); err != nil {
		t.Fatal(err)
	}

	got, seq, ok, err := r.Load(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("snapshot salvo não foi encontrado")
	}
	if seq != 42 {
		t.Errorf("folded_seq = %d, esperado 42", seq)
	}
	// == de verdade: foi por isso que o tempo no State virou int64
	if got != want {
		t.Errorf("state divergiu pelo JSONB\n got: %+v\nwant: %+v", got, want)
	}
}

// Dois folds concorrentes do mesmo pet podem terminar fora de ordem. O que
// chegar atrasado com seq menor não pode sobrescrever o mais novo, senão o
// snapshot anda pra trás e o próximo Get refolda eventos já foldados.
func TestSaveAtrasadoNaoSobrescreveOMaisNovo(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)
	pet := novoPet(t)

	novo := sim.Fold(sim.Genesis(pet, t0), []sim.Event{
		{ID: "01JB", At: t0.Add(2 * time.Hour), Kind: sim.KindInteract},
	}, sim.DefaultTuning())
	velho := sim.Fold(sim.Genesis(pet, t0), nil, sim.DefaultTuning())

	if err := r.Save(ctx, pet, novo, 100); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, pet, velho, 50); err != nil {
		t.Fatalf("save atrasado devolveu erro em vez de virar no-op: %v", err)
	}

	got, seq, _, err := r.Load(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 100 {
		t.Errorf("folded_seq = %d, o save atrasado sobrescreveu o mais novo", seq)
	}
	if got != novo {
		t.Error("state foi sobrescrito pelo save atrasado")
	}
}

func TestSaveAvancaOSnapshot(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)
	pet := novoPet(t)

	s := sim.Genesis(pet, t0)
	if err := r.Save(ctx, pet, s, 10); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, pet, s, 20); err != nil {
		t.Fatal(err)
	}
	if _, seq, _, err := r.Load(ctx, pet); err != nil || seq != 20 {
		t.Errorf("folded_seq = %d (err=%v), esperado 20", seq, err)
	}
}

// Evento atrasado invalida o snapshot: o próximo Get refolda do genesis.
// Apagar o que não existe também não pode ser erro.
func TestDeleteEhIdempotente(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)
	pet := novoPet(t)

	if err := r.Delete(ctx, pet); err != nil {
		t.Fatalf("delete de snapshot inexistente devolveu erro: %v", err)
	}

	if err := r.Save(ctx, pet, sim.Genesis(pet, t0), 10); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, pet); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := r.Load(ctx, pet); err != nil || ok {
		t.Errorf("snapshot sobreviveu ao delete (ok=%v, err=%v)", ok, err)
	}
}

// Snapshot é descartável por construção. Se uma regra do sim mudar, o
// SchemaVer sobe e todo snapshot velho tem que ser ignorado, não usado.
func TestSnapshotDeSchemaVelhoEhIgnorado(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)
	pet := novoPet(t)

	velho := sim.Genesis(pet, t0)
	velho.SchemaVer = sim.SchemaVer - 1
	if err := r.Save(ctx, pet, velho, 99); err != nil {
		t.Fatal(err)
	}

	_, _, ok, err := r.Load(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("snapshot de schema antigo foi devolvido como válido")
	}
}

// A armadilha: uma linha de schema antigo com folded_seq alto. O Load a ignora,
// então o service refolda do genesis e tenta salvar com folded_seq baixo. Se o
// WHERE só olhasse folded_seq, esse Save seria bloqueado pela linha morta e o
// pet refoldaria o log inteiro em toda leitura, para sempre, em silêncio.
func TestSnapshotDeSchemaVelhoNaoTravaAChave(t *testing.T) {
	ctx := context.Background()
	r := repo.NewSnapshotRepo(pool)
	pet := novoPet(t)

	velho := sim.Genesis(pet, t0)
	velho.SchemaVer = sim.SchemaVer - 1
	if err := r.Save(ctx, pet, velho, 9999); err != nil { // seq alto de propósito
		t.Fatal(err)
	}

	novo := sim.Fold(sim.Genesis(pet, t0), nil, sim.DefaultTuning())
	if err := r.Save(ctx, pet, novo, 5); err != nil { // seq baixo: refoldou do zero
		t.Fatal(err)
	}

	got, seq, ok, err := r.Load(ctx, pet)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("snapshot novo não gravou: a linha de schema velho travou a chave")
	}
	if seq != 5 || got != novo {
		t.Errorf("folded_seq = %d, esperado 5", seq)
	}
}

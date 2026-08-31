package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ale/fera/internal/repo"
	"github.com/ale/fera/internal/service"
	"github.com/ale/fera/internal/sig"
)

func TestDeviceResolvePeloHashDoToken(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)

	dev, pet := uuid.NewString(), uuid.NewString()
	token, err := service.NovoToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, dev, pet, service.HashToken(token), chaveDe(token), time.Now()); err != nil {
		t.Fatal(err)
	}

	gotDev, gotPet, ok, err := r.ByTokenHash(ctx, service.HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("token recém-criado não resolveu")
	}
	if gotDev != dev || gotPet != pet {
		t.Errorf("resolveu %s/%s, esperado %s/%s", gotDev, gotPet, dev, pet)
	}
}

func TestTokenDesconhecidoNaoEhErro(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)

	_, _, ok, err := r.ByTokenHash(ctx, service.HashToken("nunca-existiu"))
	if err != nil {
		t.Fatalf("token desconhecido devolveu erro: %v", err)
	}
	if ok {
		t.Error("token desconhecido resolveu pra algum device")
	}
}

// O token em claro não pode estar em lugar nenhum da tabela. Um dump de
// backup não pode virar acesso a todos os pets.
func TestOTokenEmClaroNaoEstaNaTabela(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)

	token, err := service.NovoToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, uuid.NewString(), uuid.NewString(), service.HashToken(token), chaveDe(token), time.Now()); err != nil {
		t.Fatal(err)
	}

	var achou int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM devices WHERE devices::text LIKE '%' || $1 || '%'`,
		token).Scan(&achou)
	if err != nil {
		t.Fatal(err)
	}
	if achou != 0 {
		t.Errorf("o token em claro aparece em %d linhas da tabela devices", achou)
	}
}

// O índice único no hash é o que garante que um token resolve pra no máximo
// um device. Sem ele, colisão de dados viraria ambiguidade de identidade.
func TestDoisDevicesNaoPodemCompartilharToken(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)

	token, err := service.NovoToken()
	if err != nil {
		t.Fatal(err)
	}
	hash := service.HashToken(token)

	if err := r.Create(ctx, uuid.NewString(), uuid.NewString(), hash, chaveDe(token), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, uuid.NewString(), uuid.NewString(), hash, chaveDe(token), time.Now()); err == nil {
		t.Error("o banco aceitou dois devices com o mesmo hash de token")
	}
}

// Um dono pode ter celular e bicho no mesmo pet. O schema não pode impedir.
func TestDoisDevicesPodemDividirOMesmoPet(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)
	pet := uuid.NewString()

	for range 2 {
		tk, err := service.NovoToken()
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Create(ctx, uuid.NewString(), pet, service.HashToken(tk), chaveDe(tk), time.Now()); err != nil {
			t.Fatalf("segundo device no mesmo pet foi recusado: %v", err)
		}
	}
}

func chaveDe(token string) []byte {
	k := sig.Chave(token)
	return k[:]
}

// O caminho assinado busca o device pelo id, não pelo segredo. Sem sign_key
// gravada, o device não pode assinar: é o caso dos registros antigos, que
// continuam só no Bearer.
func TestByIDDevolveAChaveDeAssinatura(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)

	dev, pet := uuid.NewString(), uuid.NewString()
	token, err := service.NovoToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, dev, pet, service.HashToken(token), chaveDe(token), time.Now()); err != nil {
		t.Fatal(err)
	}

	gotPet, chave, ok, err := r.ByID(ctx, dev)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("device recém-criado não resolveu por id")
	}
	if gotPet != pet {
		t.Errorf("pet = %q, esperado %q", gotPet, pet)
	}
	if len(chave) != 32 {
		t.Errorf("chave com %d bytes, esperado 32", len(chave))
	}

	if _, _, ok, err := r.ByID(ctx, uuid.NewString()); err != nil || ok {
		t.Errorf("device inexistente resolveu (ok=%v err=%v)", ok, err)
	}
}

// A chave de assinatura não pode ser o token, nem o hash dele: um dump de
// backup não deve entregar a credencial que o device usa pra tudo.
func TestAChaveDeAssinaturaNaoEhOToken(t *testing.T) {
	ctx := context.Background()
	r := repo.NewDeviceRepo(pool)

	token, err := service.NovoToken()
	if err != nil {
		t.Fatal(err)
	}
	dev := uuid.NewString()
	if err := r.Create(ctx, dev, uuid.NewString(), service.HashToken(token), chaveDe(token), time.Now()); err != nil {
		t.Fatal(err)
	}

	var achou int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM devices WHERE devices::text LIKE '%' || $1 || '%'`,
		token).Scan(&achou); err != nil {
		t.Fatal(err)
	}
	if achou != 0 {
		t.Errorf("o token em claro aparece em %d linhas", achou)
	}
	_, chave, _, err := r.ByID(ctx, dev)
	if err != nil {
		t.Fatal(err)
	}
	if string(chave) == string(service.HashToken(token)) {
		t.Error("a chave de assinatura é igual ao hash do Bearer: um vaza o outro")
	}
}

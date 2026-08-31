package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ale/fera/internal/sig"
)

type linhaDevice struct {
	deviceID string
	petID    string
	signKey  []byte
}

type fakeDevices struct {
	mu      sync.Mutex
	porHash map[string]linhaDevice
	porID   map[string]linhaDevice
}

func novoFakeDevices() *fakeDevices {
	return &fakeDevices{porHash: map[string]linhaDevice{}, porID: map[string]linhaDevice{}}
}

func (f *fakeDevices) Create(_ context.Context, deviceID, petID string, hash, signKey []byte, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := linhaDevice{deviceID, petID, append([]byte{}, signKey...)}
	f.porHash[string(hash)] = l
	f.porID[deviceID] = l
	return nil
}

func (f *fakeDevices) ByTokenHash(_ context.Context, hash []byte) (string, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.porHash[string(hash)]
	return r.deviceID, r.petID, ok, nil
}

func (f *fakeDevices) ByID(_ context.Context, deviceID string) (string, []byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.porID[deviceID]
	if !ok || len(r.signKey) == 0 {
		return "", nil, false, nil
	}
	return r.petID, r.signKey, true, nil
}

func idsSequenciais() func() string {
	var n int
	return func() string { n++; return "id-" + string(rune('a'+n-1)) }
}

func TestRegisterDevolveTokenQueAutentica(t *testing.T) {
	svc := NewDeviceService(novoFakeDevices(), func() time.Time { return time.Unix(0, 0) }, idsSequenciais())
	ctx := context.Background()

	reg, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Token == "" || reg.DeviceID == "" || reg.PetID == "" {
		t.Fatalf("registro incompleto: %+v", reg)
	}
	if reg.DeviceID == reg.PetID {
		t.Error("device_id e pet_id são o mesmo valor")
	}

	dev, pet, err := svc.Authenticate(ctx, reg.Token)
	if err != nil {
		t.Fatalf("o token recém-emitido não autentica: %v", err)
	}
	if dev != reg.DeviceID || pet != reg.PetID {
		t.Errorf("autenticou como %s/%s, esperado %s/%s", dev, pet, reg.DeviceID, reg.PetID)
	}
}

// O token em claro não pode existir no banco. Se existisse, um dump de backup
// entregaria acesso a todos os pets.
func TestOTokenEmClaroNuncaVaiProStore(t *testing.T) {
	store := novoFakeDevices()
	svc := NewDeviceService(store, func() time.Time { return time.Unix(0, 0) }, idsSequenciais())

	reg, err := svc.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	for hash := range store.porHash {
		if strings.Contains(hash, reg.Token) {
			t.Fatal("o token em claro foi parar no store")
		}
		if len(hash) != sha256.Size {
			t.Errorf("guardou %d bytes, esperado um SHA-256 de %d", len(hash), sha256.Size)
		}
	}
}

// Token errado, token vazio e token de outro device têm que dar todos
// ErrForbidden. Diferenciar entrega informação pra quem está sondando.
func TestTokenInvalidoSempreDaForbidden(t *testing.T) {
	svc := NewDeviceService(novoFakeDevices(), func() time.Time { return time.Unix(0, 0) }, idsSequenciais())
	ctx := context.Background()

	reg, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ nome, token string }{
		{"vazio", ""},
		{"lixo", "nao-sou-token"},
		{"prefixo do válido", reg.Token[:len(reg.Token)-1]},
		{"válido com sufixo", reg.Token + "x"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			if _, _, err := svc.Authenticate(ctx, c.token); !errors.Is(err, ErrForbidden) {
				t.Errorf("erro = %v, esperado ErrForbidden", err)
			}
		})
	}
}

// Cada registro nasce com pet próprio. Se dois registros caíssem no mesmo pet,
// um dono leria o bicho do outro.
func TestCadaRegistroTemPetProprio(t *testing.T) {
	svc := NewDeviceService(novoFakeDevices(), func() time.Time { return time.Unix(0, 0) }, idsSequenciais())
	ctx := context.Background()

	a, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.PetID == b.PetID {
		t.Errorf("dois registros no mesmo pet: %s", a.PetID)
	}
	if a.Token == b.Token {
		t.Error("dois registros com o mesmo token")
	}

	// e o token de um não abre o pet do outro
	_, pet, err := svc.Authenticate(ctx, a.Token)
	if err != nil {
		t.Fatal(err)
	}
	if pet == b.PetID {
		t.Error("o token de A resolveu pro pet de B")
	}
}

// Token de 256 bits é o que dispensa hash lento no armazenamento. Se alguém
// encolher isso, a premissa do SHA-256 sem salt cai junto.
func TestTokenTem256BitsDeEntropia(t *testing.T) {
	vistos := map[string]bool{}
	for range 100 {
		tk, err := NovoToken()
		if err != nil {
			t.Fatal(err)
		}
		if vistos[tk] {
			t.Fatal("NovoToken repetiu um valor em 100 chamadas")
		}
		vistos[tk] = true

		// base64 sem padding de 32 bytes = 43 caracteres
		if len(tk) != 43 {
			t.Fatalf("token com %d caracteres, esperado 43 (32 bytes em base64url)", len(tk))
		}
	}
}

// A assinatura substitui o Bearer no device: o segredo não cruza o fio.
func TestRequisicaoAssinadaAutentica(t *testing.T) {
	svc := NewDeviceService(novoFakeDevices(), func() time.Time { return t0 }, idsSequenciais())
	ctx := context.Background()

	reg, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}

	corpo := []byte(`{"events":[]}`)
	req := Assinada{
		DeviceID: reg.DeviceID,
		Metodo:   "POST",
		Caminho:  "/v1/pets/" + reg.PetID + "/events",
		Quando:   t0,
		Corpo:    corpo,
	}
	req.Assinatura = sig.Assinar(sig.Chave(reg.Token), req.Metodo, req.Caminho, req.Quando, corpo)

	pet, err := svc.AuthenticateSigned(ctx, req)
	if err != nil {
		t.Fatalf("a assinatura válida foi recusada: %v", err)
	}
	if pet != reg.PetID {
		t.Errorf("pet = %q, esperado %q", pet, reg.PetID)
	}
}

// Toda falha vira ErrForbidden, sem distinguir os casos: distinguir conta pra
// quem está sondando qual dos três aconteceu.
func TestAssinaturaInvalidaSempreDaForbidden(t *testing.T) {
	svc := NewDeviceService(novoFakeDevices(), func() time.Time { return t0 }, idsSequenciais())
	ctx := context.Background()
	reg, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}

	valida := func() Assinada {
		r := Assinada{
			DeviceID: reg.DeviceID, Metodo: "POST",
			Caminho: "/v1/pets/" + reg.PetID + "/events", Quando: t0, Corpo: []byte("x"),
		}
		r.Assinatura = sig.Assinar(sig.Chave(reg.Token), r.Metodo, r.Caminho, r.Quando, r.Corpo)
		return r
	}

	casos := map[string]func(*Assinada){
		"device inexistente": func(r *Assinada) { r.DeviceID = "nao-existe" },
		"device vazio":       func(r *Assinada) { r.DeviceID = "" },
		"assinatura vazia":   func(r *Assinada) { r.Assinatura = "" },
		"assinatura trocada": func(r *Assinada) {
			r.Assinatura = sig.Assinar(sig.Chave("outro"), r.Metodo, r.Caminho, r.Quando, r.Corpo)
		},
		"corpo adulterado":    func(r *Assinada) { r.Corpo = []byte("adulterado") },
		"caminho adulterado":  func(r *Assinada) { r.Caminho = "/v1/pets/outro/events" },
		"relógio muito longe": func(r *Assinada) { r.Quando = t0.Add(-72 * time.Hour) },
	}
	for nome, quebra := range casos {
		t.Run(nome, func(t *testing.T) {
			r := valida()
			quebra(&r)
			if _, err := svc.AuthenticateSigned(ctx, r); !errors.Is(err, ErrForbidden) {
				t.Errorf("erro = %v, esperado ErrForbidden", err)
			}
		})
	}
}

// Bearer e assinatura convivem: o app usa Bearer sobre HTTPS, o device assina.
// Tirar o Bearer quebraria o app; tirar a assinatura quebraria o device.
func TestBearerEAssinaturaConvivem(t *testing.T) {
	svc := NewDeviceService(novoFakeDevices(), func() time.Time { return t0 }, idsSequenciais())
	ctx := context.Background()
	reg, err := svc.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.Authenticate(ctx, reg.Token); err != nil {
		t.Errorf("o Bearer parou de funcionar: %v", err)
	}
	r := Assinada{
		DeviceID: reg.DeviceID, Metodo: "GET",
		Caminho: "/v1/pets/" + reg.PetID, Quando: t0,
	}
	r.Assinatura = sig.Assinar(sig.Chave(reg.Token), r.Metodo, r.Caminho, r.Quando, nil)
	if _, err := svc.AuthenticateSigned(ctx, r); err != nil {
		t.Errorf("a assinatura não funcionou: %v", err)
	}
}

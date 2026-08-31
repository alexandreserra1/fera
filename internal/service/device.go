package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/ale/fera/internal/sig"
)

// deviceStore fala em primitivos de propósito. Um tipo Device compartilhado
// teria que morar num dos dois pacotes, e qualquer escolha inverteria uma
// seta: ou o service importa o repo, ou o repo importa o service.
type deviceStore interface {
	Create(ctx context.Context, deviceID, petID string, tokenHash, signKey []byte, now time.Time) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (deviceID, petID string, ok bool, err error)
	ByID(ctx context.Context, deviceID string) (petID string, signKey []byte, ok bool, err error)
}

// Registration é a ÚNICA vez que o token existe em claro. Depois disto só
// existe o hash, e não há como recuperá-lo: device que perdeu o token
// registra de novo.
type Registration struct {
	DeviceID string
	PetID    string
	Token    string
}

type DeviceService struct {
	devices deviceStore
	clock   func() time.Time
	// injetáveis pra que o teste seja determinístico sem afrouxar a produção
	newID    func() string
	newToken func() (string, error)
}

func NewDeviceService(devices deviceStore, clock func() time.Time, newID func() string) *DeviceService {
	return &DeviceService{devices: devices, clock: clock, newID: newID, newToken: NovoToken}
}

// NovoToken gera 32 bytes de crypto/rand. 256 bits de entropia é o que torna
// desnecessário qualquer hash lento no armazenamento: não existe brute force
// viável pra encarecer.
func NovoToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gerar token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken é o que vai pro banco. SHA-256 sem salt, de propósito: ver a
// justificativa em migrations/0002_device_auth.sql.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Register cria device e pet de uma vez, e o pet_id é gerado AQUI, nunca
// recebido do cliente.
//
// É isso que faz a autorização significar alguma coisa: se o cliente
// escolhesse o pet_id, qualquer um se registraria no pet de outro e o token
// não protegeria nada. O pet_id é aleatório e chega ao dono junto com o token.
//
// Parear um segundo device no mesmo pet é feature separada e vai exigir provar
// posse do primeiro token. Hoje cada registro nasce com um pet novo.
func (s *DeviceService) Register(ctx context.Context) (Registration, error) {
	token, err := s.newToken()
	if err != nil {
		return Registration{}, err
	}
	reg := Registration{DeviceID: s.newID(), PetID: s.newID(), Token: token}

	// Guarda os dois: o hash pro Bearer (app) e a chave derivada pra
	// assinatura (device). Nenhum dos dois é o token.
	chave := sig.Chave(token)
	if err := s.devices.Create(ctx, reg.DeviceID, reg.PetID, HashToken(token), chave[:], s.clock()); err != nil {
		return Registration{}, err
	}
	return reg, nil
}

// Authenticate resolve o token pro device dono. Token desconhecido devolve
// ErrForbidden, nunca ErrNotFound: dizer "esse token não existe" versus "existe
// mas não pode" entrega informação de graça pra quem está sondando.
func (s *DeviceService) Authenticate(ctx context.Context, token string) (deviceID, petID string, err error) {
	if token == "" {
		return "", "", ErrForbidden
	}
	deviceID, petID, ok, err := s.devices.ByTokenHash(ctx, HashToken(token))
	if err != nil {
		return "", "", fmt.Errorf("autenticar device: %w", err)
	}
	if !ok {
		return "", "", ErrForbidden
	}
	return deviceID, petID, nil
}

// Assinada é uma requisição que chegou assinada em vez de com Bearer.
type Assinada struct {
	DeviceID   string
	Metodo     string
	Caminho    string
	Quando     time.Time
	Corpo      []byte
	Assinatura string
}

// AuthenticateSigned confere a assinatura e devolve o pet do device.
//
// Devolve ErrForbidden pra TODA falha, sem distinguir device inexistente de
// assinatura errada de relógio fora da janela: distinguir conta pra quem está
// sondando qual dos três aconteceu.
func (s *DeviceService) AuthenticateSigned(ctx context.Context, req Assinada) (string, error) {
	if req.DeviceID == "" || req.Assinatura == "" {
		return "", ErrForbidden
	}
	if !sig.DentroDaJanela(req.Quando, s.clock(), sig.JanelaPadrao) {
		return "", ErrForbidden
	}

	petID, chave, ok, err := s.devices.ByID(ctx, req.DeviceID)
	if err != nil {
		return "", fmt.Errorf("autenticar assinatura: %w", err)
	}
	if !ok || len(chave) != 32 {
		return "", ErrForbidden
	}

	var k [32]byte
	copy(k[:], chave)
	if !sig.Confere(k, req.Metodo, req.Caminho, req.Quando, req.Corpo, req.Assinatura) {
		return "", ErrForbidden
	}
	return petID, nil
}

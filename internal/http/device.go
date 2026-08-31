package httpapi

import (
	"context"
	"net/http"

	"github.com/ale/fera/internal/service"
)

type deviceService interface {
	Register(ctx context.Context) (service.Registration, error)
	Authenticate(ctx context.Context, token string) (deviceID, petID string, err error)
}

type DeviceHandler struct{ svc deviceService }

func NewDeviceHandler(svc deviceService) *DeviceHandler { return &DeviceHandler{svc: svc} }

type registerResponse struct {
	DeviceID string `json:"device_id"`
	PetID    string `json:"pet_id"`
	Token    string `json:"token"`
}

// Register é o único endpoint sem autenticação, e tem que ser: é por onde a
// credencial nasce.
//
// Não aceita pet_id do cliente. O pet_id é sorteado no servidor e volta junto
// com o token; se viesse do corpo, qualquer um se registraria no pet de outro
// e a autorização não significaria nada.
//
// 201 e não 200: criou recurso. E o token aparece UMA vez.
func (h *DeviceHandler) Register(w http.ResponseWriter, r *http.Request) {
	reg, err := h.svc.Register(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, registerResponse{
		DeviceID: reg.DeviceID,
		PetID:    reg.PetID,
		Token:    reg.Token,
	})
}

// Package httpapi is the notifications module's HTTP transport: the caller
// registers the devices that receive push notifications.
package httpapi

import (
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

type handler struct {
	cmd command.Handlers
	qry query.Handlers
}

// Register mounts /me/devices; protect authenticates the caller and resolves
// them into an access.Actor. Devices are named by their ID in URLs, never by
// their token: URLs end up in access logs.
func Register(mux *http.ServeMux, cmd command.Handlers, qry query.Handlers, protect httpx.Middleware) {
	h := handler{cmd: cmd, qry: qry}
	for pattern, e := range map[string]httpx.Endpoint{
		"POST /me/devices":        access.ForActor(h.register),
		"GET /me/devices":         access.ForActor(h.list),
		"DELETE /me/devices/{id}": access.ForActor(h.unregister),
	} {
		mux.Handle(pattern, protect(e))
	}
}

type deviceResponse struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toDevice(d *domain.Device) deviceResponse {
	return deviceResponse{
		ID:        d.ID(),
		Token:     d.Token,
		Platform:  string(d.Platform),
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

type registerRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

func (h handler) register(r *http.Request, a access.Actor) (int, any, error) {
	var req registerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return 0, nil, err
	}
	token, err := h.cmd.RegisterDevice.Handle(r.Context(), command.RegisterDevice{
		UserID:   a.UserID,
		Token:    req.Token,
		Platform: req.Platform,
	})
	if err != nil {
		return 0, nil, err
	}
	devices, err := h.qry.ListDevices.Handle(r.Context(), query.ListDevices{UserID: a.UserID})
	if err != nil {
		return 0, nil, err
	}
	for _, d := range devices {
		if d.Token == token {
			return http.StatusOK, toDevice(d), nil
		}
	}
	return 0, nil, domain.ErrDeviceNotFound // unregistered concurrently
}

func (h handler) list(r *http.Request, a access.Actor) (int, any, error) {
	devices, err := h.qry.ListDevices.Handle(r.Context(), query.ListDevices{UserID: a.UserID})
	if err != nil {
		return 0, nil, err
	}
	out := make([]deviceResponse, 0, len(devices))
	for _, d := range devices {
		out = append(out, toDevice(d))
	}
	return http.StatusOK, out, nil
}

func (h handler) unregister(r *http.Request, a access.Actor) (int, any, error) {
	cmd := command.UnregisterDevice{UserID: a.UserID, DeviceID: r.PathValue("id")}
	if err := h.cmd.UnregisterDevice.Handle(r.Context(), cmd); err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, nil
}

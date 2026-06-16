package web

import (
	"net/http"

	"github.com/firefoxx04/toyotaview/internal/obs"
)

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.Dashboard)
	mux.HandleFunc("GET /vehicles", h.VehiclesAlias)
	mux.HandleFunc("GET /vehicles/{vehicleID}", h.VehicleDetail)
	mux.HandleFunc("POST /refresh", h.RefreshAll)
	mux.HandleFunc("POST /vehicles/{vehicleID}/refresh", h.RefreshVehicle)
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)
	mux.HandleFunc("GET /version", h.Version)

	var handler http.Handler = mux
	handler = RequireSameOrigin(handler)
	handler = SecurityHeaders(handler)
	handler = obs.Middleware(h.observer)(handler)

	return handler
}

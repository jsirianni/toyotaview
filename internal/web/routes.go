package web

import (
	"net/http"

	"github.com/firefoxx04/toyotaview/internal/obs"
)

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("GET /signup", h.SignupPage)
	mux.HandleFunc("POST /signup", h.Signup)
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)
	mux.HandleFunc("GET /version", h.Version)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", h.Dashboard)
	protected.HandleFunc("GET /vehicles", h.VehiclesAlias)
	protected.HandleFunc("GET /vehicles/{vehicleID}", h.VehicleDetail)
	protected.HandleFunc("POST /refresh", h.RefreshAll)
	protected.HandleFunc("POST /vehicles/{vehicleID}/refresh", h.RefreshVehicle)
	protected.HandleFunc("POST /logout", h.Logout)
	mux.Handle("/", h.RequireLogin(protected))

	var handler http.Handler = mux
	handler = RequireSameOrigin(handler)
	handler = SecurityHeaders(handler)
	handler = obs.Middleware(h.observer)(handler)

	return handler
}

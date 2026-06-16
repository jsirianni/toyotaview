package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	"github.com/firefoxx04/toyotaview/internal/app"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/store"
	"go.uber.org/zap"
)

//go:embed templates/*.html
var _templates embed.FS

type VersionInfo struct {
	Version string
	Commit  string
	Date    string
}

type Handler struct {
	provider          app.VehicleProvider
	store             store.Reader
	logger            *zap.Logger
	observer          *obs.Observer
	dashboardTemplate *template.Template
	vehicleTemplate   *template.Template
	errorTemplate     *template.Template
	version           VersionInfo
}

func NewHandler(
	provider app.VehicleProvider,
	reader store.Reader,
	logger *zap.Logger,
	observer *obs.Observer,
	version VersionInfo,
) (*Handler, error) {
	dashboardTemplate, err := template.ParseFS(
		_templates,
		"templates/base.html",
		"templates/dashboard.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse dashboard templates: %w", err)
	}

	vehicleTemplate, err := template.ParseFS(
		_templates,
		"templates/base.html",
		"templates/vehicle.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse vehicle templates: %w", err)
	}

	errorTemplate, err := template.ParseFS(
		_templates,
		"templates/base.html",
		"templates/error.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse error templates: %w", err)
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		provider:          provider,
		store:             reader,
		logger:            logger,
		observer:          observer,
		dashboardTemplate: dashboardTemplate,
		vehicleTemplate:   vehicleTemplate,
		errorTemplate:     errorTemplate,
		version:           version,
	}, nil
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	h.renderDashboard(w, r, http.StatusOK, nil)
}

func (h *Handler) VehiclesAlias(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusMovedPermanently)
}

func (h *Handler) VehicleDetail(w http.ResponseWriter, r *http.Request) {
	vehicleID := r.PathValue("vehicleID")
	snapshot, ok := h.store.Get(vehicleID)
	if !ok {
		h.renderError(w, r, http.StatusNotFound, "Vehicle not found in cache")
		return
	}

	page := vehiclePage(snapshot, nil)
	if err := h.render(w, http.StatusOK, "vehicle", page); err != nil {
		h.logger.Error("render vehicle detail", zap.Error(err))
	}
}

func (h *Handler) RefreshAll(w http.ResponseWriter, r *http.Request) {
	_, err := h.provider.RefreshAll(r.Context())
	if err != nil {
		h.logger.Warn("refresh all failed", zap.Error(err))
		h.renderDashboard(w, r, http.StatusBadGateway, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) RefreshVehicle(w http.ResponseWriter, r *http.Request) {
	vehicleID := r.PathValue("vehicleID")
	snapshot, err := h.provider.RefreshVehicle(r.Context(), vehicleID)
	if err != nil {
		h.logger.Warn("refresh vehicle failed", zap.String("vehicle_id", vehicleID), zap.Error(err))
		if snapshot.Vehicle.ID != "" {
			page := vehiclePage(snapshot, err)
			if renderErr := h.render(w, http.StatusBadGateway, "vehicle", page); renderErr != nil {
				h.logger.Error("render vehicle refresh error", zap.Error(renderErr))
			}
			return
		}

		if cached, ok := h.store.Get(vehicleID); ok {
			page := vehiclePage(cached, err)
			if renderErr := h.render(w, http.StatusBadGateway, "vehicle", page); renderErr != nil {
				h.logger.Error("render cached vehicle refresh error", zap.Error(renderErr))
			}
			return
		}

		h.renderError(w, r, http.StatusBadGateway, userMessage(err))
		return
	}

	http.Redirect(w, r, "/vehicles/"+url.PathEscape(vehicleID), http.StatusSeeOther)
}

func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) Readyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) Version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": h.version.Version,
		"commit":  h.version.Commit,
		"date":    h.version.Date,
	})
}

func (h *Handler) renderDashboard(
	w http.ResponseWriter,
	_ *http.Request,
	statusCode int,
	err error,
) {
	page := dashboardPage(h.store.List(), errOrSnapshot(err, h.store.LastError()))
	if renderErr := h.render(w, statusCode, "dashboard", page); renderErr != nil {
		h.logger.Error("render dashboard", zap.Error(renderErr))
		if !errors.Is(renderErr, http.ErrAbortHandler) {
			http.Error(w, "template render failed", http.StatusInternalServerError)
		}
	}
}

func (h *Handler) renderError(
	w http.ResponseWriter,
	_ *http.Request,
	statusCode int,
	message string,
) {
	page := ErrorPage{
		Title:   "Error",
		Status:  statusCode,
		Message: message,
	}
	if err := h.render(w, statusCode, "error", page); err != nil {
		h.logger.Error("render error page", zap.Error(err))
		http.Error(w, message, statusCode)
	}
}

func (h *Handler) render(w http.ResponseWriter, statusCode int, name string, data any) error {
	var tmpl *template.Template
	switch name {
	case "dashboard":
		tmpl = h.dashboardTemplate
	case "vehicle":
		tmpl = h.vehicleTemplate
	case "error":
		tmpl = h.errorTemplate
	default:
		return fmt.Errorf("unknown template %q", name)
	}

	var buffer bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buffer, name, data); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)

	_, err := buffer.WriteTo(w)
	return err
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

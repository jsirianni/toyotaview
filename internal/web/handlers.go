package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/firefoxx04/toyotaview/internal/app"
	"github.com/firefoxx04/toyotaview/internal/auth"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/storage"
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

type Authenticator interface {
	Signup(ctx context.Context, username string, password string) (auth.SessionResult, error)
	Login(ctx context.Context, username string, password string) (auth.SessionResult, error)
	AuthenticateToken(ctx context.Context, token string) (storage.Session, error)
	Logout(ctx context.Context, token string) error
}

type Handler struct {
	provider          app.VehicleProvider
	store             store.Reader
	authenticator     Authenticator
	secureCookies     bool
	logger            *zap.Logger
	observer          *obs.Observer
	dashboardTemplate *template.Template
	vehicleTemplate   *template.Template
	loginTemplate     *template.Template
	signupTemplate    *template.Template
	errorTemplate     *template.Template
	version           VersionInfo
}

func NewHandler(
	provider app.VehicleProvider,
	reader store.Reader,
	authenticator Authenticator,
	secureCookies bool,
	logger *zap.Logger,
	observer *obs.Observer,
	version VersionInfo,
) (*Handler, error) {
	if authenticator == nil {
		return nil, errors.New("authenticator is required")
	}

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

	loginTemplate, err := template.ParseFS(
		_templates,
		"templates/base.html",
		"templates/login.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse login templates: %w", err)
	}

	signupTemplate, err := template.ParseFS(
		_templates,
		"templates/base.html",
		"templates/signup.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse signup templates: %w", err)
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
		authenticator:     authenticator,
		secureCookies:     secureCookies,
		logger:            logger,
		observer:          observer,
		dashboardTemplate: dashboardTemplate,
		vehicleTemplate:   vehicleTemplate,
		loginTemplate:     loginTemplate,
		signupTemplate:    signupTemplate,
		errorTemplate:     errorTemplate,
		version:           version,
	}, nil
}

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.hasValidSession(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	page := AuthPage{
		Title: "Sign in",
	}
	if err := h.render(w, http.StatusOK, "login", page); err != nil {
		h.logger.Error("render login", zap.Error(err))
	}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderAuthError(w, http.StatusBadRequest, "login", "Unable to read sign-in form.", "")
		return
	}

	username := r.FormValue("username")
	result, err := h.authenticator.Login(r.Context(), username, r.FormValue("password"))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			h.renderAuthError(
				w,
				http.StatusUnauthorized,
				"login",
				"Invalid username or password.",
				username,
			)
			return
		}

		h.logger.Error("login failed", zap.Error(err))
		h.renderError(w, r, http.StatusInternalServerError, "Unable to sign in right now.")
		return
	}

	h.setSessionCookie(w, r, result.Token, result.Session.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) SignupPage(w http.ResponseWriter, r *http.Request) {
	if h.hasValidSession(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	page := AuthPage{
		Title: "Create account",
	}
	if err := h.render(w, http.StatusOK, "signup", page); err != nil {
		h.logger.Error("render signup", zap.Error(err))
	}
}

func (h *Handler) Signup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderAuthError(w, http.StatusBadRequest, "signup", "Unable to read signup form.", "")
		return
	}

	username := r.FormValue("username")
	result, err := h.authenticator.Signup(r.Context(), username, r.FormValue("password"))
	if err != nil {
		statusCode, message, handled := signupError(err)
		if handled {
			h.renderAuthError(w, statusCode, "signup", message, username)
			return
		}

		h.logger.Error("signup failed", zap.Error(err))
		h.renderError(w, r, http.StatusInternalServerError, "Unable to create an account right now.")
		return
	}

	h.setSessionCookie(w, r, result.Token, result.Session.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err == nil {
		if logoutErr := h.authenticator.Logout(r.Context(), cookie.Value); logoutErr != nil {
			h.logger.Warn("logout failed", zap.Error(logoutErr))
		}
	}

	h.clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	case "login":
		tmpl = h.loginTemplate
	case "signup":
		tmpl = h.signupTemplate
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

func (h *Handler) renderAuthError(
	w http.ResponseWriter,
	statusCode int,
	templateName string,
	message string,
	username string,
) {
	page := AuthPage{
		Title:    authTitle(templateName),
		Error:    message,
		Username: auth.NormalizeUsername(username),
	}
	if err := h.render(w, statusCode, templateName, page); err != nil {
		h.logger.Error("render auth error", zap.String("template", templateName), zap.Error(err))
		http.Error(w, message, statusCode)
	}
}

func (h *Handler) hasValidSession(r *http.Request) bool {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return false
	}

	_, err = h.authenticator.AuthenticateToken(r.Context(), cookie.Value)
	return err == nil
}

func (h *Handler) setSessionCookie(
	w http.ResponseWriter,
	r *http.Request,
	token string,
	expiresAt time.Time,
) {
	// #nosec G124 -- loopback HTTP development must work; non-loopback or HTTPS requests set Secure.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookies || r.TLS != nil,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- loopback HTTP development must work; non-loopback or HTTPS requests set Secure.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookies || r.TLS != nil,
	})
}

func signupError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, auth.ErrUsernameTaken):
		return http.StatusConflict, "That username is unavailable.", true
	case errors.Is(err, auth.ErrInvalidUsername):
		return http.StatusBadRequest, "Username is required and must be 100 bytes or fewer.", true
	case errors.Is(err, auth.ErrInvalidPassword):
		return http.StatusBadRequest, "Password is required and must be 72 bytes or fewer.", true
	default:
		return 0, "", false
	}
}

func authTitle(templateName string) string {
	if templateName == "signup" {
		return "Create account"
	}

	return "Sign in"
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

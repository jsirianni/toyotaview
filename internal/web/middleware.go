package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/firefoxx04/toyotaview/internal/auth"
)

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")

		next.ServeHTTP(w, r)
	})
}

func RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		if !sameOrigin(r, r.Header.Get("Origin")) {
			http.Error(w, "origin mismatch", http.StatusForbidden)
			return
		}

		referer := r.Header.Get("Referer")
		if referer != "" && !sameOrigin(r, referer) {
			http.Error(w, "referer mismatch", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.CookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			redirectToLogin(w, r)
			return
		}

		if _, err := h.authenticator.AuthenticateToken(r.Context(), cookie.Value); err != nil {
			h.clearSessionCookie(w, r)
			redirectToLogin(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request, candidate string) bool {
	if strings.TrimSpace(candidate) == "" {
		return true
	}

	parsed, err := url.Parse(candidate)
	if err != nil {
		return false
	}

	return parsed.Host == r.Host
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

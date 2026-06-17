package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/auth"
	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
	"github.com/firefoxx04/toyotaview/internal/storage"
	"github.com/firefoxx04/toyotaview/internal/store"
	"go.uber.org/zap"
)

func TestDashboardDoesNotRefresh(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{}
	memoryStore := store.NewMemoryStore()
	memoryStore.Save(store.VehicleSnapshot{
		Vehicle: smartcar.Vehicle{ID: "vehicle-1", Make: "Toyota", Model: "4Runner", Year: 2024},
	})

	handler := newTestHandler(t, provider, memoryStore)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if provider.refreshAllCalls != 0 {
		t.Fatalf("refreshAllCalls = %d, want 0", provider.refreshAllCalls)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestDashboardNoDataState(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "No cached vehicle data yet") {
		t.Fatalf("body = %q, want empty state", body)
	}
}

func TestRefreshAllRedirects(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{}
	handler := newTestHandler(t, provider, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if provider.refreshAllCalls != 1 {
		t.Fatalf("refreshAllCalls = %d, want 1", provider.refreshAllCalls)
	}

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
}

func TestRefreshAllErrorVisible(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{refreshAllErr: errTest}
	handler := newTestHandler(t, provider, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "refresh failed") {
		t.Fatalf("body = %q, want refresh error", rec.Body.String())
	}
}

func TestVehicleRefresh(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{}
	memoryStore := store.NewMemoryStore()
	handler := newTestHandler(t, provider, memoryStore)
	req := httptest.NewRequest(http.MethodPost, "/vehicles/vehicle-1/refresh", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if provider.refreshVehicleCalls != 1 {
		t.Fatalf("refreshVehicleCalls = %d, want 1", provider.refreshVehicleCalls)
	}
}

func TestUnknownRoute(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodDelete, "/refresh", nil)
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Header().Get("X-Content-Type-Options") == "" {
		t.Fatal("missing X-Content-Type-Options")
	}
}

func TestOriginMismatchRejected(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Origin", "http://evil.example")
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestErrorEscapesHTML(t *testing.T) {
	t.Parallel()

	provider := &fakeProvider{refreshAllErr: errHTML}
	handler := newTestHandler(t, provider, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(body), "<script>") {
		t.Fatalf("body = %q, want escaped html", string(body))
	}
}

func TestProtectedRouteRedirectsToLogin(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	if rec.Header().Get("Location") != "/login" {
		t.Fatalf("Location = %q, want /login", rec.Header().Get("Location"))
	}
}

func TestLoginPage(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "Sign in") {
		t.Fatalf("body = %q, want sign-in page", rec.Body.String())
	}
}

func TestSignupPage(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/signup", nil)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "Create account") {
		t.Fatalf("body = %q, want signup page", rec.Body.String())
	}
}

func TestLoginSuccessRedirectsAndSetsCookie(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := formRequest(http.MethodPost, "/login", "username=driver&password=correct")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	if rec.Header().Get("Location") != "/" {
		t.Fatalf("Location = %q, want /", rec.Header().Get("Location"))
	}

	if cookie := findCookie(rec.Result().Cookies(), auth.CookieName); cookie == nil || cookie.Value == "" {
		t.Fatalf("session cookie = %#v, want non-empty cookie", cookie)
	}
}

func TestLoginFailureShowsError(t *testing.T) {
	t.Parallel()

	authenticator := newFakeAuth()
	authenticator.loginErr = auth.ErrInvalidCredentials
	handler := newTestHandlerWithAuth(t, &fakeProvider{}, store.NewMemoryStore(), authenticator)
	req := formRequest(http.MethodPost, "/login", "username=driver&password=wrong")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "Invalid username or password") {
		t.Fatalf("body = %q, want invalid login error", rec.Body.String())
	}
}

func TestSignupSuccessRedirectsAndSetsCookie(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	req := formRequest(http.MethodPost, "/signup", "username=driver&password=correct")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	if rec.Header().Get("Location") != "/" {
		t.Fatalf("Location = %q, want /", rec.Header().Get("Location"))
	}

	if cookie := findCookie(rec.Result().Cookies(), auth.CookieName); cookie == nil || cookie.Value == "" {
		t.Fatalf("session cookie = %#v, want non-empty cookie", cookie)
	}
}

func TestSignupDuplicateShowsError(t *testing.T) {
	t.Parallel()

	authenticator := newFakeAuth()
	authenticator.signupErr = auth.ErrUsernameTaken
	handler := newTestHandlerWithAuth(t, &fakeProvider{}, store.NewMemoryStore(), authenticator)
	req := formRequest(http.MethodPost, "/signup", "username=driver&password=correct")
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "username is unavailable") {
		t.Fatalf("body = %q, want duplicate username error", rec.Body.String())
	}
}

func TestLogoutClearsSession(t *testing.T) {
	t.Parallel()

	authenticator := newFakeAuth()
	handler := newTestHandlerWithAuth(t, &fakeProvider{}, store.NewMemoryStore(), authenticator)
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	addAuthCookie(req)
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	if rec.Header().Get("Location") != "/login" {
		t.Fatalf("Location = %q, want /login", rec.Header().Get("Location"))
	}

	if authenticator.logoutToken != "valid-token" {
		t.Fatalf("logoutToken = %q, want valid-token", authenticator.logoutToken)
	}

	cookie := findCookie(rec.Result().Cookies(), auth.CookieName)
	if cookie == nil || cookie.MaxAge != -1 {
		t.Fatalf("session cookie = %#v, want cleared cookie", cookie)
	}
}

func TestPublicHealthAndVersionRoutes(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeProvider{}, store.NewMemoryStore())
	for _, path := range []string{"/healthz", "/readyz", "/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.Routes().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func newTestHandler(t *testing.T, provider *fakeProvider, reader store.Reader) *Handler {
	t.Helper()

	return newTestHandlerWithAuth(t, provider, reader, newFakeAuth())
}

func newTestHandlerWithAuth(
	t *testing.T,
	provider *fakeProvider,
	reader store.Reader,
	authenticator Authenticator,
) *Handler {
	t.Helper()

	observer, err := obs.New(context.Background(), config.OTELConfig{
		Enabled:        false,
		ServiceName:    "test",
		ServiceVersion: "dev",
		Timeout:        time.Second,
		SampleRatio:    1,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("obs.New() error = %v", err)
	}

	handler, err := NewHandler(
		provider,
		reader,
		authenticator,
		false,
		zap.NewNop(),
		observer,
		VersionInfo{
			Version: "test",
			Commit:  "abc123",
			Date:    "2026-06-16",
		},
	)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	return handler
}

func addAuthCookie(req *http.Request) {
	req.AddCookie(&http.Cookie{
		Name:  auth.CookieName,
		Value: "valid-token",
	})
}

func formRequest(method string, path string, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}

type fakeProvider struct {
	refreshAllCalls     int
	refreshVehicleCalls int
	refreshAllErr       error
	refreshVehicleErr   error
}

type fakeAuth struct {
	validTokens map[string]storage.Session
	loginErr    error
	signupErr   error
	logoutToken string
}

func newFakeAuth() *fakeAuth {
	expiresAt := time.Now().Add(time.Hour)
	return &fakeAuth{
		validTokens: map[string]storage.Session{
			"valid-token": {
				TokenHash: auth.TokenHash("valid-token"),
				UserID:    1,
				ExpiresAt: expiresAt,
				CreatedAt: time.Now(),
			},
		},
	}
}

func (f *fakeAuth) Signup(context.Context, string, string) (auth.SessionResult, error) {
	if f.signupErr != nil {
		return auth.SessionResult{}, f.signupErr
	}

	return auth.SessionResult{
		Token: "signup-token",
		Session: storage.Session{
			TokenHash: auth.TokenHash("signup-token"),
			UserID:    1,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}, nil
}

func (f *fakeAuth) Login(context.Context, string, string) (auth.SessionResult, error) {
	if f.loginErr != nil {
		return auth.SessionResult{}, f.loginErr
	}

	return auth.SessionResult{
		Token: "login-token",
		Session: storage.Session{
			TokenHash: auth.TokenHash("login-token"),
			UserID:    1,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}, nil
}

func (f *fakeAuth) AuthenticateToken(
	_ context.Context,
	token string,
) (storage.Session, error) {
	session, ok := f.validTokens[token]
	if !ok {
		return storage.Session{}, auth.ErrInvalidSession
	}

	return session, nil
}

func (f *fakeAuth) Logout(_ context.Context, token string) error {
	f.logoutToken = token
	delete(f.validTokens, token)

	return nil
}

func (f *fakeProvider) RefreshAll(context.Context) ([]store.VehicleSnapshot, error) {
	f.refreshAllCalls++
	if f.refreshAllErr != nil {
		return nil, f.refreshAllErr
	}

	return nil, nil
}

func (f *fakeProvider) RefreshVehicle(context.Context, string) (store.VehicleSnapshot, error) {
	f.refreshVehicleCalls++
	if f.refreshVehicleErr != nil {
		return store.VehicleSnapshot{}, f.refreshVehicleErr
	}

	return store.VehicleSnapshot{
		Vehicle: smartcar.Vehicle{ID: "vehicle-1"},
	}, nil
}

var (
	errTest = errors.New("refresh failed")
	errHTML = errors.New("<script>alert(1)</script>")
)

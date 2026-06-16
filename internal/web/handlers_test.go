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

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
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
	rec := httptest.NewRecorder()

	handler.Routes().ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(body), "<script>") {
		t.Fatalf("body = %q, want escaped html", string(body))
	}
}

func newTestHandler(t *testing.T, provider *fakeProvider, reader store.Reader) *Handler {
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

	handler, err := NewHandler(provider, reader, zap.NewNop(), observer, VersionInfo{
		Version: "test",
		Commit:  "abc123",
		Date:    "2026-06-16",
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	return handler
}

type fakeProvider struct {
	refreshAllCalls     int
	refreshVehicleCalls int
	refreshAllErr       error
	refreshVehicleErr   error
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

package smartcar

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"go.uber.org/zap"
)

func TestClientTokenCaching(t *testing.T) {
	t.Parallel()

	var tokenHits atomic.Int64
	var vehicleHits atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			tokenHits.Add(1)
			if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/x-www-form-urlencoded") {
				t.Fatalf("Content-Type = %q, want form encoded", got)
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "token-1",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		case r.URL.Path == "/vehicles/vehicle-1":
			vehicleHits.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
				t.Fatalf("Authorization = %q, want bearer token", got)
			}
			_ = json.NewEncoder(w).Encode(vehicleResponse{
				Data: vehicleData{
					ID: "vehicle-1",
					Attributes: vehicleAttributes{
						Make:           "Toyota",
						Model:          "4Runner",
						Year:           2024,
						PowertrainType: "ICE",
						Mode:           "live",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)

	for range 2 {
		if _, err := client.GetVehicle(context.Background(), "vehicle-1"); err != nil {
			t.Fatalf("GetVehicle() error = %v", err)
		}
	}

	if tokenHits.Load() != 1 {
		t.Fatalf("token hits = %d, want 1", tokenHits.Load())
	}

	if vehicleHits.Load() != 2 {
		t.Fatalf("vehicle hits = %d, want 2", vehicleHits.Load())
	}
}

func TestClientListConnectionsUsesUserFilter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "token-1",
				ExpiresIn:   3600,
			})
		case "/connections":
			if got := r.URL.Query().Get("filter[userId]"); got != "user-1" {
				t.Fatalf("filter[userId] = %q, want user-1", got)
			}
			_ = json.NewEncoder(w).Encode(connectionsResponse{
				Data: []connectionData{
					{
						ID: "conn-1",
						Attributes: connectionAttributes{
							Vehicle: vehicleAttributes{
								Make:           "Toyota",
								Model:          "Tacoma",
								Year:           2025,
								PowertrainType: "ICE",
								Mode:           "live",
							},
						},
						Relationships: connectionRelationSet{
							Vehicle: resourceRelationship{Data: resourceID{ID: "vehicle-1", Type: "vehicle"}},
							User:    resourceRelationship{Data: resourceID{ID: "user-1", Type: "user"}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	connections, err := client.ListConnections(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ListConnections() error = %v", err)
	}

	if len(connections) != 1 {
		t.Fatalf("len(connections) = %d, want 1", len(connections))
	}
}

func TestClientGetSignalHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "token-1",
				ExpiresIn:   3600,
			})
		case "/vehicles/vehicle-1/signals/service-records":
			if got := r.Header.Get("sc-user-id"); got != "user-1" {
				t.Fatalf("sc-user-id = %q, want user-1", got)
			}
			if got := r.Header.Get("SC-Unit-System"); got != "imperial" {
				t.Fatalf("SC-Unit-System = %q, want imperial", got)
			}
			w.Header().Set("SC-Fetched-At", "2026-06-16T12:00:00Z")
			w.Header().Set("SC-Data-Age", "2026-06-16T11:00:00Z")
			_, _ = w.Write([]byte(`{"value":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	signal, err := client.GetSignal(context.Background(), "user-1", "vehicle-1", "service-records")
	if err != nil {
		t.Fatalf("GetSignal() error = %v", err)
	}

	if signal.Code != "service-records" {
		t.Fatalf("Signal.Code = %q, want service-records", signal.Code)
	}

	if signal.RetrievedAt.IsZero() {
		t.Fatal("Signal.RetrievedAt = zero, want timestamp")
	}
}

func TestClientRetry503(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "token-1",
				ExpiresIn:   3600,
			})
		case "/vehicles/vehicle-1":
			if attempts.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"retry"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(vehicleResponse{
				Data: vehicleData{
					ID: "vehicle-1",
					Attributes: vehicleAttributes{
						Make:           "Toyota",
						Model:          "4Runner",
						Year:           2024,
						PowertrainType: "ICE",
						Mode:           "live",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	if _, err := client.GetVehicle(context.Background(), "vehicle-1"); err != nil {
		t.Fatalf("GetVehicle() error = %v", err)
	}

	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

func TestClient401NoRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "token-1",
				ExpiresIn:   3600,
			})
		case "/vehicles/vehicle-1":
			attempts.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	if _, err := client.GetVehicle(context.Background(), "vehicle-1"); err == nil {
		t.Fatal("GetVehicle() error = nil, want error")
	}

	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}

func TestClientContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	if _, err := client.accessToken(ctx); err == nil {
		t.Fatal("accessToken() error = nil, want context error")
	}
}

func TestClientErrorBodyTruncated(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "token-1",
				ExpiresIn:   3600,
			})
		case "/vehicles/vehicle-1":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(strings.Repeat("x", _maxErrorBodyBytes+1024)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.URL)
	_, err := client.GetVehicle(context.Background(), "vehicle-1")
	if err == nil {
		t.Fatal("GetVehicle() error = nil, want error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T, want APIError", err)
	}

	if len(apiErr.Body) != _maxErrorBodyBytes {
		t.Fatalf("len(apiErr.Body) = %d, want %d", len(apiErr.Body), _maxErrorBodyBytes)
	}
}

func newTestClient(t *testing.T, iamBaseURL string, vehicleBaseURL string) *Client {
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

	httpClient := NewHTTPClient(2*time.Second, &tls.Config{MinVersion: tls.VersionTLS12})
	client, err := NewClient(httpClient, config.SmartcarConfig{
		ClientID:       "client-1",
		ClientSecret:   "secret-1",
		IAMBaseURL:     iamBaseURL,
		VehicleBaseURL: vehicleBaseURL,
		UnitSystem:     "imperial",
		Timeout:        2 * time.Second,
		MaxRetries:     1,
	}, "test", zap.NewNop(), observer)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return client
}

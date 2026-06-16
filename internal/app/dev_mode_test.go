package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/devsmartcar"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
	"github.com/firefoxx04/toyotaview/internal/store"
	"go.uber.org/zap"
)

func TestRefreshAllWithDevSmartcarHappy(t *testing.T) {
	t.Parallel()

	service := newDevModeService(t, devsmartcar.ScenarioHappy, nil)

	snapshots, err := service.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}

	if len(snapshots) != 2 {
		t.Fatalf("len(snapshots) = %d, want 2", len(snapshots))
	}

	for _, snapshot := range snapshots {
		if snapshot.Vehicle.Make != "Toyota" {
			t.Fatalf("snapshot.Vehicle.Make = %q, want Toyota", snapshot.Vehicle.Make)
		}

		if snapshot.Partial {
			t.Fatalf("snapshot.Partial = true, want false for %s", snapshot.Vehicle.ID)
		}
	}
}

func TestRefreshAllWithDevSmartcarPartial(t *testing.T) {
	t.Parallel()

	service := newDevModeService(t, devsmartcar.ScenarioPartial, nil)

	snapshots, err := service.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}

	fourRunner := findSnapshot(t, snapshots, "dev-4runner")
	if !fourRunner.Partial {
		t.Fatal("fourRunner.Partial = false, want true")
	}

	if fourRunner.Signals["diagnostics-dtclist"].State != smartcar.SignalStateUnsupported {
		t.Fatalf(
			"diagnostics-dtclist state = %q, want %q",
			fourRunner.Signals["diagnostics-dtclist"].State,
			smartcar.SignalStateUnsupported,
		)
	}

	if fourRunner.Signals["service-isinservice"].State != smartcar.SignalStateUnavailable {
		t.Fatalf(
			"service-isinservice state = %q, want %q",
			fourRunner.Signals["service-isinservice"].State,
			smartcar.SignalStateUnavailable,
		)
	}

	if fourRunner.Signals["service-records"].State != smartcar.SignalStateError {
		t.Fatalf(
			"service-records state = %q, want %q",
			fourRunner.Signals["service-records"].State,
			smartcar.SignalStateError,
		)
	}

	tacoma := findSnapshot(t, snapshots, "dev-tacoma")
	if tacoma.Partial {
		t.Fatal("tacoma.Partial = true, want false")
	}
}

func TestRefreshAllWithDevSmartcarEmpty(t *testing.T) {
	t.Parallel()

	service := newDevModeService(t, devsmartcar.ScenarioEmpty, nil)

	if _, err := service.RefreshAll(context.Background()); !errors.Is(err, ErrNoVehicles) {
		t.Fatalf("RefreshAll() error = %v, want ErrNoVehicles", err)
	}
}

func TestRefreshAllWithDevSmartcarFailure(t *testing.T) {
	t.Parallel()

	service := newDevModeService(t, devsmartcar.ScenarioFailure, nil)

	if _, err := service.RefreshAll(context.Background()); err == nil {
		t.Fatal("RefreshAll() error = nil, want error")
	}
}

func TestRefreshVehicleWithDevSmartcarAllowlist(t *testing.T) {
	t.Parallel()

	service := newDevModeService(t, devsmartcar.ScenarioHappy, func(cfg *config.SmartcarConfig) {
		cfg.VehicleIDs = []string{"dev-4runner"}
	})

	snapshot, err := service.RefreshVehicle(context.Background(), "dev-4runner")
	if err != nil {
		t.Fatalf("RefreshVehicle() error = %v", err)
	}

	if snapshot.Vehicle.ID != "dev-4runner" {
		t.Fatalf("snapshot.Vehicle.ID = %q, want dev-4runner", snapshot.Vehicle.ID)
	}
}

func newDevModeService(
	t *testing.T,
	scenario string,
	option func(*config.SmartcarConfig),
) *Service {
	t.Helper()

	cfg := config.SmartcarConfig{
		UserID: "",
		SignalCodes: []string{
			"odometer-traveleddistance",
			"diagnostics-dtclist",
			"service-isinservice",
			"service-records",
		},
	}
	if option != nil {
		option(&cfg)
	}

	api, err := devsmartcar.New(devsmartcar.Config{
		Scenario:    scenario,
		SignalCodes: cfg.SignalCodes,
		UnitSystem:  "imperial",
	})
	if err != nil {
		t.Fatalf("devsmartcar.New() error = %v", err)
	}

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

	return NewService(cfg, api, store.NewMemoryStore(), zap.NewNop(), observer)
}

func findSnapshot(t *testing.T, snapshots []store.VehicleSnapshot, vehicleID string) store.VehicleSnapshot {
	t.Helper()

	for _, snapshot := range snapshots {
		if snapshot.Vehicle.ID == vehicleID {
			return snapshot
		}
	}

	t.Fatalf("vehicle %q not found in snapshots", vehicleID)
	return store.VehicleSnapshot{}
}

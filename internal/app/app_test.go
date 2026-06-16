package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
	"github.com/firefoxx04/toyotaview/internal/store"
	"go.uber.org/zap"
)

func TestRefreshAllSuccess(t *testing.T) {
	t.Parallel()

	service := newTestService(t, fakeAPI{
		connections: []smartcar.Connection{
			{VehicleID: "vehicle-1"},
		},
		vehicles: map[string]smartcar.Vehicle{
			"vehicle-1": {ID: "vehicle-1", Make: "Toyota"},
		},
		signals: map[string]smartcar.Signal{
			"vehicle-1:service-records": {Code: "service-records"},
		},
	})

	snapshots, err := service.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}

	if len(snapshots) != 1 {
		t.Fatalf("len(snapshots) = %d, want 1", len(snapshots))
	}
}

func TestRefreshAllAllowlist(t *testing.T) {
	t.Parallel()

	service := newTestService(t, fakeAPI{
		connections: []smartcar.Connection{
			{VehicleID: "vehicle-1"},
			{VehicleID: "vehicle-2"},
		},
		vehicles: map[string]smartcar.Vehicle{
			"vehicle-1": {ID: "vehicle-1", Make: "Toyota"},
		},
		signals: map[string]smartcar.Signal{
			"vehicle-1:service-records": {Code: "service-records"},
		},
	}, func(cfg *config.SmartcarConfig) {
		cfg.VehicleIDs = []string{"vehicle-1"}
	})

	snapshots, err := service.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}

	if len(snapshots) != 1 || snapshots[0].Vehicle.ID != "vehicle-1" {
		t.Fatalf("snapshots = %+v, want only vehicle-1", snapshots)
	}
}

func TestRefreshAllNoVehicles(t *testing.T) {
	t.Parallel()

	service := newTestService(t, fakeAPI{})
	if _, err := service.RefreshAll(context.Background()); !errors.Is(err, ErrNoVehicles) {
		t.Fatalf("RefreshAll() error = %v, want ErrNoVehicles", err)
	}
}

func TestRefreshVehicleRejectsNotAllowed(t *testing.T) {
	t.Parallel()

	service := newTestService(t, fakeAPI{}, func(cfg *config.SmartcarConfig) {
		cfg.VehicleIDs = []string{"vehicle-1"}
	})

	if _, err := service.RefreshVehicle(context.Background(), "vehicle-2"); !errors.Is(err, ErrVehicleNotAllowed) {
		t.Fatalf("RefreshVehicle() error = %v, want ErrVehicleNotAllowed", err)
	}
}

func TestRefreshAllPartialSignalFailure(t *testing.T) {
	t.Parallel()

	service := newTestService(t, fakeAPI{
		connections: []smartcar.Connection{{VehicleID: "vehicle-1"}},
		vehicles: map[string]smartcar.Vehicle{
			"vehicle-1": {ID: "vehicle-1", Make: "Toyota"},
		},
		signals: map[string]smartcar.Signal{},
		signalErrors: map[string]error{
			"vehicle-1:service-records": &smartcar.APIError{Status: 404},
		},
	})

	snapshots, err := service.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}

	if !snapshots[0].Partial {
		t.Fatal("snapshot.Partial = false, want true")
	}
}

type fakeAPI struct {
	connections  []smartcar.Connection
	vehicles     map[string]smartcar.Vehicle
	signals      map[string]smartcar.Signal
	vehicleError map[string]error
	signalErrors map[string]error
}

func (f fakeAPI) ListConnections(context.Context, string) ([]smartcar.Connection, error) {
	return append([]smartcar.Connection(nil), f.connections...), nil
}

func (f fakeAPI) GetVehicle(context.Context, string) (smartcar.Vehicle, error) {
	return smartcar.Vehicle{}, errors.New("unexpected GetVehicle call")
}

func (f fakeAPI) GetSignal(context.Context, string, string, string) (smartcar.Signal, error) {
	return smartcar.Signal{}, errors.New("unexpected GetSignal call")
}

func (f fakeAPI) getVehicle(vehicleID string) (smartcar.Vehicle, error) {
	if err := f.vehicleError[vehicleID]; err != nil {
		return smartcar.Vehicle{}, err
	}

	vehicle, ok := f.vehicles[vehicleID]
	if !ok {
		return smartcar.Vehicle{}, errors.New("vehicle missing")
	}

	return vehicle, nil
}

func (f fakeAPI) getSignal(vehicleID string, signalCode string) (smartcar.Signal, error) {
	key := vehicleID + ":" + signalCode
	if err := f.signalErrors[key]; err != nil {
		return smartcar.Signal{}, err
	}

	signal, ok := f.signals[key]
	if !ok {
		return smartcar.Signal{}, errors.New("signal missing")
	}

	return signal, nil
}

type fakeAPIAdapter struct {
	fakeAPI
}

func (f fakeAPIAdapter) GetVehicle(_ context.Context, vehicleID string) (smartcar.Vehicle, error) {
	return f.fakeAPI.getVehicle(vehicleID)
}

func (f fakeAPIAdapter) GetSignal(
	_ context.Context,
	_ string,
	vehicleID string,
	signalCode string,
) (smartcar.Signal, error) {
	return f.fakeAPI.getSignal(vehicleID, signalCode)
}

func newTestService(
	t *testing.T,
	api fakeAPI,
	options ...func(*config.SmartcarConfig),
) *Service {
	t.Helper()

	cfg := config.SmartcarConfig{
		UserID:      "user-1",
		SignalCodes: []string{"service-records"},
	}
	for _, option := range options {
		option(&cfg)
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

	return NewService(cfg, fakeAPIAdapter{fakeAPI: api}, store.NewMemoryStore(), zap.NewNop(), observer)
}

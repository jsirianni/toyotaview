package devsmartcar

import (
	"context"
	"errors"
	"testing"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
)

func TestNew_InvalidScenario(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{Scenario: "nope"}); err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestHappyScenario(t *testing.T) {
	t.Parallel()

	api, err := New(Config{
		Scenario:    ScenarioHappy,
		SignalCodes: config.DefaultSignalCodes(),
		UnitSystem:  "imperial",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	connections, err := api.ListConnections(context.Background(), "")
	if err != nil {
		t.Fatalf("ListConnections() error = %v", err)
	}

	if len(connections) != 2 {
		t.Fatalf("len(connections) = %d, want 2", len(connections))
	}

	if connections[0].UserID != _defaultUserID {
		t.Fatalf("connections[0].UserID = %q, want %q", connections[0].UserID, _defaultUserID)
	}

	signal, err := api.GetSignal(
		context.Background(),
		_defaultUserID,
		"dev-4runner",
		"odometer-traveleddistance",
	)
	if err != nil {
		t.Fatalf("GetSignal() error = %v", err)
	}

	if signal.Name == "" || signal.Group == "" {
		t.Fatalf("signal = %+v, want name and group", signal)
	}

	if signal.Unit != "mi" {
		t.Fatalf("signal.Unit = %q, want mi", signal.Unit)
	}

	if len(signal.Body) == 0 {
		t.Fatal("signal.Body is empty, want payload")
	}
}

func TestPartialScenario(t *testing.T) {
	t.Parallel()

	api, err := New(Config{
		Scenario: ScenarioPartial,
		SignalCodes: []string{
			"odometer-traveleddistance",
			"diagnostics-dtclist",
			"service-isinservice",
			"service-records",
		},
		UnitSystem: "imperial",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := api.GetSignal(
		context.Background(),
		_defaultUserID,
		"dev-4runner",
		"diagnostics-dtclist",
	); err == nil {
		t.Fatal("GetSignal() error = nil, want unsupported error")
	}

	if _, err := api.GetSignal(
		context.Background(),
		_defaultUserID,
		"dev-4runner",
		"service-isinservice",
	); err == nil {
		t.Fatal("GetSignal() error = nil, want unavailable error")
	}

	err = mustSignalError(
		api,
		"dev-4runner",
		"service-records",
	)
	if err == nil {
		t.Fatal("mustSignalError() = nil, want error")
	}

	var apiErr *smartcar.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 500 {
		t.Fatalf("error = %v, want APIError status 500", err)
	}

	if _, err := api.GetSignal(
		context.Background(),
		_defaultUserID,
		"dev-tacoma",
		"service-records",
	); err != nil {
		t.Fatalf("GetSignal() error = %v, want success for second vehicle", err)
	}
}

func TestEmptyScenario(t *testing.T) {
	t.Parallel()

	api, err := New(Config{Scenario: ScenarioEmpty})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	connections, err := api.ListConnections(context.Background(), _defaultUserID)
	if err != nil {
		t.Fatalf("ListConnections() error = %v", err)
	}

	if len(connections) != 0 {
		t.Fatalf("len(connections) = %d, want 0", len(connections))
	}

	if _, err := api.GetVehicle(context.Background(), "dev-4runner"); err == nil {
		t.Fatal("GetVehicle() error = nil, want not found")
	}
}

func TestFailureScenario(t *testing.T) {
	t.Parallel()

	api, err := New(Config{Scenario: ScenarioFailure})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := api.ListConnections(context.Background(), _defaultUserID); err == nil {
		t.Fatal("ListConnections() error = nil, want error")
	}
}

func TestDefensiveCopies(t *testing.T) {
	t.Parallel()

	api, err := New(Config{
		Scenario:    ScenarioHappy,
		SignalCodes: config.DefaultSignalCodes(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	connections, err := api.ListConnections(context.Background(), _defaultUserID)
	if err != nil {
		t.Fatalf("ListConnections() error = %v", err)
	}

	connections[0].Permissions[0] = "changed"

	again, err := api.ListConnections(context.Background(), _defaultUserID)
	if err != nil {
		t.Fatalf("ListConnections() error = %v", err)
	}

	if again[0].Permissions[0] == "changed" {
		t.Fatal("ListConnections() returned shared slice data")
	}

	signal, err := api.GetSignal(
		context.Background(),
		_defaultUserID,
		"dev-4runner",
		"service-records",
	)
	if err != nil {
		t.Fatalf("GetSignal() error = %v", err)
	}

	signal.Body[0] = 'X'

	againSignal, err := api.GetSignal(
		context.Background(),
		_defaultUserID,
		"dev-4runner",
		"service-records",
	)
	if err != nil {
		t.Fatalf("GetSignal() error = %v", err)
	}

	if len(againSignal.Body) > 0 && againSignal.Body[0] == 'X' {
		t.Fatal("GetSignal() returned shared body data")
	}
}

func mustSignalError(api *API, vehicleID string, signalCode string) error {
	_, err := api.GetSignal(context.Background(), _defaultUserID, vehicleID, signalCode)
	return err
}

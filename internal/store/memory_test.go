package store

import (
	"errors"
	"testing"

	"github.com/firefoxx04/toyotaview/internal/smartcar"
)

func TestMemoryStore_SaveGet(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	snapshot := VehicleSnapshot{
		Vehicle: smartcar.Vehicle{ID: "vehicle-1", Make: "Toyota"},
		Signals: map[string]SignalSnapshot{
			"service-records": {
				Signal: smartcar.Signal{
					Code: "service-records",
					Body: []byte(`{"value":true}`),
				},
			},
		},
	}

	store.Save(snapshot)

	got, ok := store.Get("vehicle-1")
	if !ok {
		t.Fatal("Get() ok = false, want true")
	}

	got.Signals["service-records"] = SignalSnapshot{}
	again, ok := store.Get("vehicle-1")
	if !ok {
		t.Fatal("Get() second ok = false, want true")
	}

	if again.Vehicle.Make != "Toyota" {
		t.Fatalf("Vehicle.Make = %q, want Toyota", again.Vehicle.Make)
	}

	if _, ok := again.Signals["service-records"]; !ok {
		t.Fatal("Signals mutated internal store state")
	}
}

func TestMemoryStore_SaveManyList(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	store.SaveMany([]VehicleSnapshot{
		{Vehicle: smartcar.Vehicle{ID: "b"}},
		{Vehicle: smartcar.Vehicle{ID: "a"}},
	})

	got := store.List()
	if len(got) != 2 {
		t.Fatalf("List() len = %d, want 2", len(got))
	}

	if got[0].Vehicle.ID != "a" || got[1].Vehicle.ID != "b" {
		t.Fatalf("List() order = %q, %q, want a, b", got[0].Vehicle.ID, got[1].Vehicle.ID)
	}
}

func TestMemoryStore_LastError(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	errGive := errors.New("boom")
	store.SetLastError(errGive)

	if !errors.Is(store.LastError(), errGive) {
		t.Fatalf("LastError() = %v, want %v", store.LastError(), errGive)
	}
}

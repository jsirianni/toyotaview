package store

import (
	"sort"
	"sync"

	"github.com/firefoxx04/toyotaview/internal/smartcar"
)

type VehicleSnapshot = smartcar.VehicleSnapshot
type SignalSnapshot = smartcar.SignalSnapshot

type Reader interface {
	Get(vehicleID string) (VehicleSnapshot, bool)
	List() []VehicleSnapshot
	LastError() error
}

type Writer interface {
	Save(snapshot VehicleSnapshot)
	SaveMany(snapshots []VehicleSnapshot)
	SetLastError(err error)
}

type Store interface {
	Reader
	Writer
}

type MemoryStore struct {
	mu        sync.RWMutex
	vehicles  map[string]VehicleSnapshot
	lastError error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		vehicles: make(map[string]VehicleSnapshot),
	}
}

func (m *MemoryStore) Save(snapshot VehicleSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.vehicles[snapshot.Vehicle.ID] = cloneVehicleSnapshot(snapshot)
}

func (m *MemoryStore) SaveMany(snapshots []VehicleSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, snapshot := range snapshots {
		m.vehicles[snapshot.Vehicle.ID] = cloneVehicleSnapshot(snapshot)
	}
}

func (m *MemoryStore) Get(vehicleID string) (VehicleSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot, ok := m.vehicles[vehicleID]
	if !ok {
		return VehicleSnapshot{}, false
	}

	return cloneVehicleSnapshot(snapshot), true
}

func (m *MemoryStore) List() []VehicleSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshots := make([]VehicleSnapshot, 0, len(m.vehicles))
	for _, snapshot := range m.vehicles {
		snapshots = append(snapshots, cloneVehicleSnapshot(snapshot))
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Vehicle.ID < snapshots[j].Vehicle.ID
	})

	return snapshots
}

func (m *MemoryStore) LastError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.lastError
}

func (m *MemoryStore) SetLastError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastError = err
}

func cloneVehicleSnapshot(snapshot VehicleSnapshot) VehicleSnapshot {
	cloned := snapshot
	cloned.Signals = make(map[string]SignalSnapshot, len(snapshot.Signals))
	for code, signalSnapshot := range snapshot.Signals {
		clonedSignal := signalSnapshot
		clonedSignal.Signal.Body = cloneBytes(signalSnapshot.Signal.Body)
		cloned.Signals[code] = clonedSignal
	}

	return cloned
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}

	cloned := make([]byte, len(value))
	copy(cloned, value)

	return cloned
}

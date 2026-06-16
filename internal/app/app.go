package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
	"github.com/firefoxx04/toyotaview/internal/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

var (
	ErrNoVehicles        = errors.New("no vehicles found for configured Smartcar user")
	ErrVehicleNotAllowed = errors.New("requested vehicle is not in the configured allowlist")
)

type VehicleProvider interface {
	RefreshAll(ctx context.Context) ([]store.VehicleSnapshot, error)
	RefreshVehicle(ctx context.Context, vehicleID string) (store.VehicleSnapshot, error)
}

var _ VehicleProvider = (*Service)(nil)

type Service struct {
	cfg               config.SmartcarConfig
	api               smartcar.API
	store             store.Store
	logger            *zap.Logger
	observer          *obs.Observer
	allowedVehicleIDs map[string]struct{}
}

func NewService(
	cfg config.SmartcarConfig,
	api smartcar.API,
	store store.Store,
	logger *zap.Logger,
	observer *obs.Observer,
) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	allowedVehicleIDs := make(map[string]struct{}, len(cfg.VehicleIDs))
	for _, vehicleID := range cfg.VehicleIDs {
		allowedVehicleIDs[vehicleID] = struct{}{}
	}

	return &Service{
		cfg:               cfg,
		api:               api,
		store:             store,
		logger:            logger,
		observer:          observer,
		allowedVehicleIDs: allowedVehicleIDs,
	}
}

func (s *Service) RefreshAll(ctx context.Context) ([]store.VehicleSnapshot, error) {
	startedAt := time.Now()
	ctx, span := s.observer.Tracer().Start(ctx, "refresh_all")
	defer span.End()

	connections, err := s.api.ListConnections(ctx, s.cfg.UserID)
	if err != nil {
		s.store.SetLastError(err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "list connections failed")
		s.observer.RecordRefresh(ctx, "all", "error", time.Since(startedAt))
		return nil, err
	}

	selected := s.filterConnections(connections)
	if len(selected) == 0 {
		s.store.SetLastError(ErrNoVehicles)
		span.RecordError(ErrNoVehicles)
		span.SetStatus(codes.Error, "no vehicles")
		s.observer.RecordRefresh(ctx, "all", "error", time.Since(startedAt))
		return nil, ErrNoVehicles
	}

	snapshots := make([]store.VehicleSnapshot, 0, len(selected))
	failed := 0
	for _, connection := range selected {
		snapshot := s.refreshVehicleSnapshot(ctx, connection.VehicleID)
		snapshots = append(snapshots, snapshot)
		if snapshot.Err != nil && !snapshot.Partial {
			failed++
		}
	}

	s.store.SaveMany(snapshots)
	s.store.SetLastError(nil)
	s.observer.SetCachedVehicles(len(s.store.List()))
	span.SetAttributes(attribute.Int("smartcar.vehicle_count", len(snapshots)))

	result := "ok"
	if failed == len(snapshots) {
		err = fmt.Errorf("%d of %d vehicles failed to refresh", failed, len(snapshots))
		s.store.SetLastError(err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "all vehicles failed")
		result = "error"
	} else if failed > 0 {
		result = "partial"
	}
	s.observer.RecordRefresh(ctx, "all", result, time.Since(startedAt))

	if err != nil {
		return snapshots, err
	}

	return snapshots, nil
}

func (s *Service) RefreshVehicle(
	ctx context.Context,
	vehicleID string,
) (store.VehicleSnapshot, error) {
	startedAt := time.Now()
	ctx, span := s.observer.Tracer().Start(ctx, "refresh_vehicle")
	defer span.End()

	span.SetAttributes(attribute.String("smartcar.vehicle_id", vehicleID))

	if len(s.allowedVehicleIDs) > 0 {
		if _, ok := s.allowedVehicleIDs[vehicleID]; !ok {
			s.store.SetLastError(ErrVehicleNotAllowed)
			span.RecordError(ErrVehicleNotAllowed)
			span.SetStatus(codes.Error, "vehicle not allowed")
			s.observer.RecordRefresh(ctx, "vehicle", "error", time.Since(startedAt))
			return store.VehicleSnapshot{}, ErrVehicleNotAllowed
		}
	} else {
		connections, err := s.api.ListConnections(ctx, s.cfg.UserID)
		if err != nil {
			s.store.SetLastError(err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "list connections failed")
			s.observer.RecordRefresh(ctx, "vehicle", "error", time.Since(startedAt))
			return store.VehicleSnapshot{}, err
		}

		found := false
		for _, connection := range connections {
			if connection.VehicleID == vehicleID {
				found = true
				break
			}
		}
		if !found {
			err := fmt.Errorf("%w: %s", ErrNoVehicles, vehicleID)
			s.store.SetLastError(err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "vehicle missing")
			s.observer.RecordRefresh(ctx, "vehicle", "error", time.Since(startedAt))
			return store.VehicleSnapshot{}, err
		}
	}

	snapshot := s.refreshVehicleSnapshot(ctx, vehicleID)
	s.store.Save(snapshot)
	if snapshot.Err != nil && !snapshot.Partial {
		s.store.SetLastError(snapshot.Err)
		span.RecordError(snapshot.Err)
		span.SetStatus(codes.Error, "vehicle refresh failed")
		s.observer.RecordRefresh(ctx, "vehicle", "error", time.Since(startedAt))
		return snapshot, snapshot.Err
	}

	s.store.SetLastError(nil)
	s.observer.SetCachedVehicles(len(s.store.List()))
	result := "ok"
	if snapshot.Partial {
		result = "partial"
	}
	s.observer.RecordRefresh(ctx, "vehicle", result, time.Since(startedAt))

	return snapshot, nil
}

func (s *Service) refreshVehicleSnapshot(
	ctx context.Context,
	vehicleID string,
) store.VehicleSnapshot {
	vehicle, err := s.api.GetVehicle(ctx, vehicleID)
	if err != nil {
		s.logger.Warn("vehicle refresh failed",
			zap.String("vehicle_id", vehicleID),
			zap.Error(err),
		)
		return store.VehicleSnapshot{
			Vehicle: smartcar.Vehicle{ID: vehicleID},
			Err:     err,
		}
	}

	signals := make(map[string]store.SignalSnapshot, len(s.cfg.SignalCodes))
	signalSuccesses := 0
	signalFailures := 0

	for _, signalCode := range s.cfg.SignalCodes {
		signal, signalErr := s.api.GetSignal(ctx, s.cfg.UserID, vehicleID, signalCode)
		if signalErr != nil {
			state := mapSignalState(signalErr)
			signals[signalCode] = store.SignalSnapshot{
				Signal: smartcar.Signal{
					Code:  signalCode,
					Name:  humanize(signalCode),
					Group: groupOf(signalCode),
				},
				Err:   signalErr,
				State: state,
			}
			signalFailures++
			s.logger.Warn("signal refresh failed",
				zap.String("vehicle_id", vehicleID),
				zap.String("signal_code", signalCode),
				zap.String("state", string(state)),
				zap.Error(signalErr),
			)
			continue
		}

		signals[signalCode] = store.SignalSnapshot{
			Signal: signal,
			State:  smartcar.SignalStateOK,
		}
		signalSuccesses++
	}

	snapshot := store.VehicleSnapshot{
		Vehicle:     vehicle,
		Signals:     signals,
		RefreshedAt: time.Now().UTC(),
	}

	if signalFailures > 0 {
		snapshot.Partial = true
		snapshot.Err = fmt.Errorf("%d of %d signals failed", signalFailures, len(s.cfg.SignalCodes))
	}

	if signalSuccesses == 0 && signalFailures > 0 {
		snapshot.Partial = true
	}

	return snapshot
}

func (s *Service) filterConnections(connections []smartcar.Connection) []smartcar.Connection {
	if len(s.allowedVehicleIDs) == 0 {
		return append([]smartcar.Connection(nil), connections...)
	}

	filtered := make([]smartcar.Connection, 0, len(connections))
	for _, connection := range connections {
		if _, ok := s.allowedVehicleIDs[connection.VehicleID]; ok {
			filtered = append(filtered, connection)
		}
	}

	slices.SortFunc(filtered, func(a smartcar.Connection, b smartcar.Connection) int {
		return strings.Compare(a.VehicleID, b.VehicleID)
	})

	return filtered
}

func mapSignalState(err error) smartcar.SignalState {
	var apiErr *smartcar.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusNotFound, http.StatusNotImplemented, http.StatusBadRequest:
			return smartcar.SignalStateUnsupported
		case http.StatusForbidden, http.StatusConflict, 430, http.StatusNotAcceptable:
			return smartcar.SignalStateUnavailable
		}
	}

	return smartcar.SignalStateError
}

func humanize(code string) string {
	parts := strings.Split(code, "-")
	for i, part := range parts {
		parts[i] = titleWord(part)
	}

	return strings.Join(parts, " ")
}

func groupOf(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) == 0 {
		return "Signal"
	}

	return titleWord(parts[0])
}

func titleWord(value string) string {
	if value == "" {
		return value
	}

	return strings.ToUpper(value[:1]) + value[1:]
}

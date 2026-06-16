package devsmartcar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/firefoxx04/toyotaview/internal/smartcar"
)

const (
	ScenarioHappy   = "happy"
	ScenarioPartial = "partial"
	ScenarioEmpty   = "empty"
	ScenarioFailure = "failure"

	_defaultUnitSystem = "imperial"
	_defaultUserID     = "dev-user"
	_mockBaseURL       = "https://mock.smartcar.local"
)

type Config struct {
	Scenario    string
	SignalCodes []string
	UnitSystem  string
}

type API struct {
	scenario        string
	partialOutcomes map[string]signalOutcome
	unitSystem      string
}

type signalOutcome string

const (
	signalOutcomeSuccess     signalOutcome = "success"
	signalOutcomeUnsupported signalOutcome = "unsupported"
	signalOutcomeUnavailable signalOutcome = "unavailable"
	signalOutcomeError       signalOutcome = "error"
)

var _ smartcar.API = (*API)(nil)

func New(cfg Config) (*API, error) {
	scenario := normalizeScenario(cfg.Scenario)
	if err := validateScenario(scenario); err != nil {
		return nil, err
	}

	unitSystem := strings.TrimSpace(cfg.UnitSystem)
	if unitSystem == "" {
		unitSystem = _defaultUnitSystem
	}

	return &API{
		scenario:        scenario,
		partialOutcomes: buildPartialOutcomes(cfg.SignalCodes),
		unitSystem:      unitSystem,
	}, nil
}

func (a *API) ListConnections(ctx context.Context, userID string) ([]smartcar.Connection, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	switch a.scenario {
	case ScenarioEmpty:
		return nil, nil
	case ScenarioFailure:
		return nil, newAPIError(
			"list_connections",
			http.MethodGet,
			_mockBaseURL+"/connections",
			http.StatusBadGateway,
			"dev-list-connections",
			`{"message":"mock Smartcar backend unavailable"}`,
			errors.New("mock Smartcar backend unavailable"),
		)
	default:
		return cloneConnections(builtInConnections(userID)), nil
	}
}

func (a *API) GetVehicle(ctx context.Context, vehicleID string) (smartcar.Vehicle, error) {
	if err := contextError(ctx); err != nil {
		return smartcar.Vehicle{}, err
	}

	if a.scenario == ScenarioFailure {
		return smartcar.Vehicle{}, newAPIError(
			"get_vehicle",
			http.MethodGet,
			fmt.Sprintf("%s/vehicles/%s", _mockBaseURL, vehicleID),
			http.StatusServiceUnavailable,
			"dev-get-vehicle",
			`{"message":"mock Smartcar vehicle lookup failed"}`,
			errors.New("mock Smartcar vehicle lookup failed"),
		)
	}

	vehicle, ok := builtInVehicle(vehicleID)
	if !ok || a.scenario == ScenarioEmpty {
		return smartcar.Vehicle{}, newAPIError(
			"get_vehicle",
			http.MethodGet,
			fmt.Sprintf("%s/vehicles/%s", _mockBaseURL, vehicleID),
			http.StatusNotFound,
			"dev-get-vehicle-not-found",
			`{"message":"vehicle not found"}`,
			nil,
		)
	}

	return vehicle, nil
}

func (a *API) GetSignal(
	ctx context.Context,
	_ string,
	vehicleID string,
	signalCode string,
) (smartcar.Signal, error) {
	if err := contextError(ctx); err != nil {
		return smartcar.Signal{}, err
	}

	if a.scenario == ScenarioFailure {
		return smartcar.Signal{}, newAPIError(
			"get_signal",
			http.MethodGet,
			fmt.Sprintf("%s/vehicles/%s/signals/%s", _mockBaseURL, vehicleID, signalCode),
			http.StatusServiceUnavailable,
			"dev-get-signal",
			`{"message":"mock Smartcar signal lookup failed"}`,
			errors.New("mock Smartcar signal lookup failed"),
		)
	}

	if _, ok := builtInVehicle(vehicleID); !ok || a.scenario == ScenarioEmpty {
		return smartcar.Signal{}, newAPIError(
			"get_signal",
			http.MethodGet,
			fmt.Sprintf("%s/vehicles/%s/signals/%s", _mockBaseURL, vehicleID, signalCode),
			http.StatusNotFound,
			"dev-get-signal-not-found",
			`{"message":"vehicle not found"}`,
			nil,
		)
	}

	if a.scenario == ScenarioPartial && vehicleID == "dev-4runner" {
		switch a.partialOutcomes[signalCode] {
		case signalOutcomeUnsupported:
			return smartcar.Signal{}, newAPIError(
				"get_signal",
				http.MethodGet,
				fmt.Sprintf("%s/vehicles/%s/signals/%s", _mockBaseURL, vehicleID, signalCode),
				http.StatusNotFound,
				"dev-signal-unsupported",
				`{"message":"signal unsupported by mocked vehicle"}`,
				nil,
			)
		case signalOutcomeUnavailable:
			return smartcar.Signal{}, newAPIError(
				"get_signal",
				http.MethodGet,
				fmt.Sprintf("%s/vehicles/%s/signals/%s", _mockBaseURL, vehicleID, signalCode),
				430,
				"dev-signal-unavailable",
				`{"message":"signal temporarily unavailable in mocked vehicle state"}`,
				nil,
			)
		case signalOutcomeError:
			return smartcar.Signal{}, newAPIError(
				"get_signal",
				http.MethodGet,
				fmt.Sprintf("%s/vehicles/%s/signals/%s", _mockBaseURL, vehicleID, signalCode),
				http.StatusInternalServerError,
				"dev-signal-error",
				`{"message":"mock Smartcar backend error"}`,
				errors.New("mock Smartcar backend error"),
			)
		}
	}

	return newSignal(vehicleID, signalCode, a.unitSystem, time.Now().UTC()), nil
}

func SupportedScenarios() []string {
	return []string{
		ScenarioHappy,
		ScenarioPartial,
		ScenarioEmpty,
		ScenarioFailure,
	}
}

func buildPartialOutcomes(signalCodes []string) map[string]signalOutcome {
	uniqueCodes := uniqueSignalCodes(signalCodes)
	outcomes := make(map[string]signalOutcome, 3)

	assignPreferredOutcome(
		outcomes,
		uniqueCodes,
		signalOutcomeUnsupported,
		[]string{"diagnostics-dtclist", "diagnostics-brakefluid"},
	)
	assignPreferredOutcome(
		outcomes,
		uniqueCodes,
		signalOutcomeUnavailable,
		[]string{"service-isinservice", "internalcombustionengine-fuellevel"},
	)
	assignPreferredOutcome(
		outcomes,
		uniqueCodes,
		signalOutcomeError,
		[]string{"service-records", "internalcombustionengine-range"},
	)

	fallbacks := []signalOutcome{
		signalOutcomeUnsupported,
		signalOutcomeUnavailable,
		signalOutcomeError,
	}

	nextFallback := 0
	for _, code := range uniqueCodes {
		if _, ok := outcomes[code]; ok {
			continue
		}

		for nextFallback < len(fallbacks) {
			outcome := fallbacks[nextFallback]
			nextFallback++
			if hasOutcome(outcomes, outcome) {
				continue
			}

			outcomes[code] = outcome
			break
		}

		if nextFallback >= len(fallbacks) {
			break
		}
	}

	return outcomes
}

func assignPreferredOutcome(
	outcomes map[string]signalOutcome,
	signalCodes []string,
	outcome signalOutcome,
	preferredCodes []string,
) {
	if hasOutcome(outcomes, outcome) {
		return
	}

	for _, preferredCode := range preferredCodes {
		if !contains(signalCodes, preferredCode) {
			continue
		}

		if _, taken := outcomes[preferredCode]; taken {
			continue
		}

		outcomes[preferredCode] = outcome
		return
	}
}

func hasOutcome(outcomes map[string]signalOutcome, outcome signalOutcome) bool {
	for _, existing := range outcomes {
		if existing == outcome {
			return true
		}
	}

	return false
}

func uniqueSignalCodes(signalCodes []string) []string {
	seen := make(map[string]struct{}, len(signalCodes))
	codes := make([]string, 0, len(signalCodes))
	for _, signalCode := range signalCodes {
		signalCode = strings.TrimSpace(signalCode)
		if signalCode == "" {
			continue
		}

		if _, ok := seen[signalCode]; ok {
			continue
		}

		seen[signalCode] = struct{}{}
		codes = append(codes, signalCode)
	}

	return codes
}

func newSignal(
	vehicleID string,
	signalCode string,
	unitSystem string,
	now time.Time,
) smartcar.Signal {
	value, unit := signalValue(vehicleID, signalCode, unitSystem)
	payload := map[string]any{
		"value":  value,
		"status": "ok",
	}
	if unit != "" {
		payload["unit"] = unit
	}

	body := marshalBody(payload)

	return smartcar.Signal{
		Code:         signalCode,
		Name:         signalName(signalCode),
		Group:        signalGroup(signalCode),
		Status:       "ok",
		Unit:         unit,
		Value:        value,
		Body:         body,
		RetrievedAt:  now.Add(-15 * time.Second),
		OEMUpdatedAt: now.Add(-2 * time.Minute),
		IngestedAt:   now,
	}
}

func signalValue(vehicleID string, signalCode string, unitSystem string) (any, string) {
	profile := vehicleProfile(vehicleID)

	switch signalCode {
	case "odometer-traveleddistance":
		if unitSystem == "metric" {
			return round1(profile.odometerMiles * 1.60934), "km"
		}
		return round1(profile.odometerMiles), "mi"
	case "internalcombustionengine-fuellevel":
		return profile.fuelLevelPercent, "%"
	case "internalcombustionengine-amountremaining":
		if unitSystem == "metric" {
			return round1(profile.fuelGallons * 3.78541), "L"
		}
		return round1(profile.fuelGallons), "gal"
	case "internalcombustionengine-range":
		if unitSystem == "metric" {
			return round1(profile.rangeMiles * 1.60934), "km"
		}
		return round1(profile.rangeMiles), "mi"
	case "internalcombustionengine-oillife":
		return profile.oilLifePercent, "%"
	case "diagnostics-dtccount":
		return profile.dtcCount, ""
	case "diagnostics-dtclist":
		return append([]string(nil), profile.dtcList...), ""
	case "diagnostics-mil":
		return profile.malfunctionIndicatorOn, ""
	case "diagnostics-brakefluid":
		return profile.brakeFluidStatus, ""
	case "diagnostics-oilpressure":
		if unitSystem == "metric" {
			return round1(profile.oilPressurePSI * 6.89476), "kPa"
		}
		return round1(profile.oilPressurePSI), "psi"
	case "diagnostics-oiltemperature":
		if unitSystem == "metric" {
			return round1((profile.oilTemperatureF - 32) * 5 / 9), "C"
		}
		return round1(profile.oilTemperatureF), "F"
	case "diagnostics-tirepressure":
		if unitSystem == "metric" {
			return map[string]any{
				"frontLeft":  round1(profile.tirePressurePSI["frontLeft"] * 6.89476),
				"frontRight": round1(profile.tirePressurePSI["frontRight"] * 6.89476),
				"rearLeft":   round1(profile.tirePressurePSI["rearLeft"] * 6.89476),
				"rearRight":  round1(profile.tirePressurePSI["rearRight"] * 6.89476),
			}, "kPa"
		}

		return map[string]any{
			"frontLeft":  round1(profile.tirePressurePSI["frontLeft"]),
			"frontRight": round1(profile.tirePressurePSI["frontRight"]),
			"rearLeft":   round1(profile.tirePressurePSI["rearLeft"]),
			"rearRight":  round1(profile.tirePressurePSI["rearRight"]),
		}, "psi"
	case "service-isinservice":
		return profile.inService, ""
	case "service-records":
		return []map[string]any{
			{
				"type":      "oil_change",
				"performed": profile.lastServiceDate,
				"dealer":    "Mock Toyota Service",
			},
			{
				"type":      "tire_rotation",
				"performed": profile.lastRotationDate,
				"dealer":    "Mock Toyota Service",
			},
		}, ""
	default:
		return map[string]any{
			"mocked":    true,
			"vehicleID": vehicleID,
			"signal":    signalCode,
		}, ""
	}
}

func builtInConnections(userID string) []smartcar.Connection {
	if strings.TrimSpace(userID) == "" {
		userID = _defaultUserID
	}

	createdAt := time.Date(2025, time.January, 10, 14, 30, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.June, 15, 10, 45, 0, 0, time.UTC)
	lastUsedAt := time.Date(2026, time.June, 16, 12, 0, 0, 0, time.UTC)

	vehicles := []smartcar.Vehicle{
		{
			ID:             "dev-4runner",
			Make:           "Toyota",
			Model:          "4Runner",
			Year:           2024,
			PowertrainType: "ice",
			Mode:           "off",
		},
		{
			ID:             "dev-tacoma",
			Make:           "Toyota",
			Model:          "Tacoma",
			Year:           2025,
			PowertrainType: "hybrid",
			Mode:           "on",
		},
	}

	return []smartcar.Connection{
		{
			ID:          "dev-connection-4runner",
			VehicleID:   vehicles[0].ID,
			UserID:      userID,
			Permissions: []string{"read_vehicle_info", "read_odometer", "read_vin"},
			Vehicle:     vehicles[0],
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
			LastUsedAt:  lastUsedAt,
		},
		{
			ID:          "dev-connection-tacoma",
			VehicleID:   vehicles[1].ID,
			UserID:      userID,
			Permissions: []string{"read_vehicle_info", "read_odometer", "read_engine_oil"},
			Vehicle:     vehicles[1],
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
			LastUsedAt:  lastUsedAt,
		},
	}
}

func builtInVehicle(vehicleID string) (smartcar.Vehicle, bool) {
	switch vehicleID {
	case "dev-4runner":
		return smartcar.Vehicle{
			ID:             "dev-4runner",
			Make:           "Toyota",
			Model:          "4Runner",
			Year:           2024,
			PowertrainType: "ice",
			Mode:           "off",
		}, true
	case "dev-tacoma":
		return smartcar.Vehicle{
			ID:             "dev-tacoma",
			Make:           "Toyota",
			Model:          "Tacoma",
			Year:           2025,
			PowertrainType: "hybrid",
			Mode:           "on",
		}, true
	default:
		return smartcar.Vehicle{}, false
	}
}

func cloneConnections(connections []smartcar.Connection) []smartcar.Connection {
	cloned := make([]smartcar.Connection, 0, len(connections))
	for _, connection := range connections {
		cloned = append(cloned, smartcar.Connection{
			ID:          connection.ID,
			VehicleID:   connection.VehicleID,
			UserID:      connection.UserID,
			Permissions: append([]string(nil), connection.Permissions...),
			Vehicle:     connection.Vehicle,
			CreatedAt:   connection.CreatedAt,
			UpdatedAt:   connection.UpdatedAt,
			LastUsedAt:  connection.LastUsedAt,
		})
	}

	return cloned
}

func normalizeScenario(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ScenarioHappy
	}

	return value
}

func validateScenario(value string) error {
	switch value {
	case ScenarioHappy, ScenarioPartial, ScenarioEmpty, ScenarioFailure:
		return nil
	default:
		return fmt.Errorf("invalid dev scenario %q", value)
	}
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func newAPIError(
	operation string,
	method string,
	url string,
	status int,
	requestID string,
	body string,
	err error,
) error {
	return &smartcar.APIError{
		Operation: operation,
		Method:    method,
		URL:       url,
		Status:    status,
		RequestID: requestID,
		Body:      body,
		Err:       err,
	}
}

func marshalBody(payload map[string]any) json.RawMessage {
	body, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`{"status":"error","value":"mock-payload-encoding-failed"}`)
	}

	return body
}

func signalName(code string) string {
	parts := strings.Split(code, "-")
	for i, part := range parts {
		parts[i] = titleWord(part)
	}

	return strings.Join(parts, " ")
}

func signalGroup(code string) string {
	parts := strings.SplitN(code, "-", 2)
	if len(parts) == 0 || parts[0] == "" {
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func round1(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}

type vehicleTelemetry struct {
	odometerMiles          float64
	fuelLevelPercent       float64
	fuelGallons            float64
	rangeMiles             float64
	oilLifePercent         int
	dtcCount               int
	dtcList                []string
	malfunctionIndicatorOn bool
	brakeFluidStatus       string
	oilPressurePSI         float64
	oilTemperatureF        float64
	tirePressurePSI        map[string]float64
	inService              bool
	lastServiceDate        string
	lastRotationDate       string
}

func vehicleProfile(vehicleID string) vehicleTelemetry {
	switch vehicleID {
	case "dev-tacoma":
		return vehicleTelemetry{
			odometerMiles:          18902.4,
			fuelLevelPercent:       74.0,
			fuelGallons:            14.8,
			rangeMiles:             362.5,
			oilLifePercent:         91,
			dtcCount:               0,
			dtcList:                nil,
			malfunctionIndicatorOn: false,
			brakeFluidStatus:       "normal",
			oilPressurePSI:         44.8,
			oilTemperatureF:        196.2,
			tirePressurePSI: map[string]float64{
				"frontLeft":  34.1,
				"frontRight": 34.4,
				"rearLeft":   33.7,
				"rearRight":  33.8,
			},
			inService:        false,
			lastServiceDate:  "2026-04-18",
			lastRotationDate: "2026-04-18",
		}
	default:
		return vehicleTelemetry{
			odometerMiles:          48215.7,
			fuelLevelPercent:       58.0,
			fuelGallons:            11.9,
			rangeMiles:             287.6,
			oilLifePercent:         83,
			dtcCount:               0,
			dtcList:                nil,
			malfunctionIndicatorOn: false,
			brakeFluidStatus:       "normal",
			oilPressurePSI:         42.3,
			oilTemperatureF:        201.4,
			tirePressurePSI: map[string]float64{
				"frontLeft":  32.6,
				"frontRight": 32.8,
				"rearLeft":   33.1,
				"rearRight":  33.0,
			},
			inService:        false,
			lastServiceDate:  "2026-03-22",
			lastRotationDate: "2026-03-22",
		}
	}
}

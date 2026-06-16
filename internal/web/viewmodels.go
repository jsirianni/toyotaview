package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/firefoxx04/toyotaview/internal/store"
)

type DashboardPage struct {
	Title       string
	Error       string
	Vehicles    []VehiclePage
	GeneratedAt string
}

type VehiclePage struct {
	Title       string
	Error       string
	VehicleID   string
	Make        string
	Model       string
	Year        int
	Powertrain  string
	Mode        string
	RefreshedAt string
	Partial     bool
	Signals     []SignalView
}

type SignalView struct {
	Code       string
	Name       string
	Group      string
	State      string
	Status     string
	Unit       string
	Value      string
	UpdatedAt  string
	IngestedAt string
	Error      string
	Body       string
}

type ErrorPage struct {
	Title   string
	Status  int
	Message string
}

func dashboardPage(snapshots []store.VehicleSnapshot, err error) DashboardPage {
	vehicles := make([]VehiclePage, 0, len(snapshots))
	for _, snapshot := range snapshots {
		vehicles = append(vehicles, vehiclePage(snapshot, nil))
	}

	return DashboardPage{
		Title:       "Toyota Vehicle Dashboard",
		Error:       userMessage(err),
		Vehicles:    vehicles,
		GeneratedAt: time.Now().Format(time.RFC3339),
	}
}

func vehiclePage(snapshot store.VehicleSnapshot, err error) VehiclePage {
	signals := make([]SignalView, 0, len(snapshot.Signals))
	for _, code := range sortedSignalCodes(snapshot.Signals) {
		signal := snapshot.Signals[code]
		signals = append(signals, SignalView{
			Code:       code,
			Name:       signal.Signal.Name,
			Group:      signal.Signal.Group,
			State:      string(signal.State),
			Status:     signal.Signal.Status,
			Unit:       signal.Signal.Unit,
			Value:      displayValue(signal),
			UpdatedAt:  formatTime(signal.Signal.OEMUpdatedAt),
			IngestedAt: formatTime(signal.Signal.IngestedAt),
			Error:      userMessage(signal.Err),
			Body:       prettyBody(signal.Signal.Body),
		})
	}

	title := strings.TrimSpace(fmt.Sprintf("%d %s %s", snapshot.Vehicle.Year, snapshot.Vehicle.Make, snapshot.Vehicle.Model))
	if title == "0" || title == "" {
		title = snapshot.Vehicle.ID
	}

	return VehiclePage{
		Title:       title,
		Error:       userMessage(errOrSnapshot(err, snapshot.Err)),
		VehicleID:   snapshot.Vehicle.ID,
		Make:        snapshot.Vehicle.Make,
		Model:       snapshot.Vehicle.Model,
		Year:        snapshot.Vehicle.Year,
		Powertrain:  snapshot.Vehicle.PowertrainType,
		Mode:        snapshot.Vehicle.Mode,
		RefreshedAt: formatTime(snapshot.RefreshedAt),
		Partial:     snapshot.Partial,
		Signals:     signals,
	}
}

func userMessage(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func displayValue(signal store.SignalSnapshot) string {
	if signal.Signal.Value == nil {
		return ""
	}

	switch value := signal.Signal.Value.(type) {
	case string:
		if signal.Signal.Unit == "" {
			return value
		}
		return value + " " + signal.Signal.Unit
	case bool:
		return fmt.Sprintf("%t", value)
	case float64, int, int64, uint64:
		if signal.Signal.Unit == "" {
			return fmt.Sprintf("%v", value)
		}
		return fmt.Sprintf("%v %s", value, signal.Signal.Unit)
	default:
		return prettyAny(value)
	}
}

func prettyAny(value any) string {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}

	return string(payload)
}

func prettyBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var buffer bytes.Buffer
	if err := json.Indent(&buffer, body, "", "  "); err != nil {
		return string(body)
	}

	return buffer.String()
}

func sortedSignalCodes(signals map[string]store.SignalSnapshot) []string {
	codes := make([]string, 0, len(signals))
	for code := range signals {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.Format(time.RFC3339)
}

func errOrSnapshot(primary error, fallback error) error {
	if primary != nil {
		return primary
	}

	return fallback
}

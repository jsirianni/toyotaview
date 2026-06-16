package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, requiredEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Addr != _defaultAddr {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, _defaultAddr)
	}

	if cfg.Smartcar.UnitSystem != _defaultUnitSystem {
		t.Fatalf("UnitSystem = %q, want %q", cfg.Smartcar.UnitSystem, _defaultUnitSystem)
	}

	if len(cfg.Smartcar.SignalCodes) != len(_defaultSignalCodes) {
		t.Fatalf("SignalCodes len = %d, want %d", len(cfg.Smartcar.SignalCodes), len(_defaultSignalCodes))
	}
}

func TestLoad_EnvOverridesDefaults(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	env["SC4R_ADDR"] = "127.0.0.1:9090"
	env["SC4R_SMARTCAR_UNIT_SYSTEM"] = "metric"

	cfg, err := Load(nil, env)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Addr != "127.0.0.1:9090" {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:9090")
	}

	if cfg.Smartcar.UnitSystem != "metric" {
		t.Fatalf("UnitSystem = %q, want metric", cfg.Smartcar.UnitSystem)
	}
}

func TestLoad_FlagsOverrideEnv(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	env["SC4R_ADDR"] = "127.0.0.1:9090"

	cfg, err := Load([]string{"--addr", "127.0.0.1:9191"}, env)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Addr != "127.0.0.1:9191" {
		t.Fatalf("Addr = %q, want %q", cfg.Addr, "127.0.0.1:9191")
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	t.Parallel()

	_, err := Load(nil, map[string]string{})
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	env["SC4R_READ_TIMEOUT"] = "nope"

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "SC4R_READ_TIMEOUT") {
		t.Fatalf("Load() error = %v, want duration parse error", err)
	}
}

func TestLoad_InvalidBool(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	env["SC4R_LOG_COMPRESS"] = "nope"

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "SC4R_LOG_COMPRESS") {
		t.Fatalf("Load() error = %v, want bool parse error", err)
	}
}

func TestLoad_InvalidURL(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	env["SC4R_SMARTCAR_IAM_BASE_URL"] = "://bad"

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "smartcar iam base url") {
		t.Fatalf("Load() error = %v, want url validation error", err)
	}
}

func TestLoad_CSVParsing(t *testing.T) {
	t.Parallel()

	env := requiredEnv()

	cfg, err := Load(
		[]string{
			"--smartcar-vehicle-ids", " a, ,b ,, c ",
			"--smartcar-signal-codes", "one, two, ,three",
		},
		env,
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := strings.Join(cfg.Smartcar.VehicleIDs, ","), "a,b,c"; got != want {
		t.Fatalf("VehicleIDs = %q, want %q", got, want)
	}

	if got, want := strings.Join(cfg.Smartcar.SignalCodes, ","), "one,two,three"; got != want {
		t.Fatalf("SignalCodes = %q, want %q", got, want)
	}
}

func TestRedacted(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, requiredEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	redacted := cfg.Redacted()
	if redacted.Smartcar.ClientSecret == cfg.Smartcar.ClientSecret {
		t.Fatal("ClientSecret was not redacted")
	}

	if redacted.Smartcar.UserID != "*****123456" {
		t.Fatalf("UserID = %q, want %q", redacted.Smartcar.UserID, "*****123456")
	}
}

func TestPrintConfigPathLoads(t *testing.T) {
	t.Parallel()

	cfg, err := Load([]string{"--print-config"}, requiredEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.PrintConfig {
		t.Fatal("PrintConfig = false, want true")
	}
}

func TestLoad_DevModeWithoutCredentials(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, map[string]string{
		"SC4R_DEV_MODE": "true",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Dev.Enabled {
		t.Fatal("Dev.Enabled = false, want true")
	}

	if cfg.Dev.Scenario != _defaultDevScenario {
		t.Fatalf("Dev.Scenario = %q, want %q", cfg.Dev.Scenario, _defaultDevScenario)
	}
}

func TestLoad_DevModeScenarioOverride(t *testing.T) {
	t.Parallel()

	cfg, err := Load(
		[]string{"--dev-scenario", "partial"},
		map[string]string{
			"SC4R_DEV_MODE":     "true",
			"SC4R_DEV_SCENARIO": "happy",
		},
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Dev.Scenario != "partial" {
		t.Fatalf("Dev.Scenario = %q, want partial", cfg.Dev.Scenario)
	}
}

func TestLoad_InvalidDevScenario(t *testing.T) {
	t.Parallel()

	_, err := Load(nil, map[string]string{
		"SC4R_DEV_MODE":     "true",
		"SC4R_DEV_SCENARIO": "nope",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid dev scenario") {
		t.Fatalf("Load() error = %v, want invalid dev scenario", err)
	}
}

func TestPrintConfigPathLoadsInDevModeWithoutCredentials(t *testing.T) {
	t.Parallel()

	cfg, err := Load([]string{"--print-config", "--dev-mode"}, map[string]string{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.PrintConfig {
		t.Fatal("PrintConfig = false, want true")
	}

	if !cfg.Dev.Enabled {
		t.Fatal("Dev.Enabled = false, want true")
	}
}

func TestValidateSampleRatio(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, requiredEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cfg.OTEL.SampleRatio = 1.5
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
}

func TestIsLoopback(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, requiredEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.IsLoopback() {
		t.Fatal("IsLoopback() = false, want true")
	}
}

func TestDurationDefault(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, requiredEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ReadTimeout != 15*time.Second {
		t.Fatalf("ReadTimeout = %s, want 15s", cfg.ReadTimeout)
	}
}

func requiredEnv() map[string]string {
	return map[string]string{
		"SC4R_SMARTCAR_CLIENT_ID":     "client-123456",
		"SC4R_SMARTCAR_CLIENT_SECRET": "secret-123456",
		"SC4R_SMARTCAR_USER_ID":       "user-123456",
	}
}

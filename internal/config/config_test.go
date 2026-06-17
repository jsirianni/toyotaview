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

	if cfg.Storage.Driver != StorageDriverSQLite {
		t.Fatalf("Storage.Driver = %q, want %q", cfg.Storage.Driver, StorageDriverSQLite)
	}

	if cfg.Storage.SQLite.Path != _defaultSQLitePath {
		t.Fatalf("SQLite.Path = %q, want %q", cfg.Storage.SQLite.Path, _defaultSQLitePath)
	}

	if cfg.Auth.SessionTTL != _defaultAuthSessionTTL {
		t.Fatalf("Auth.SessionTTL = %s, want %s", cfg.Auth.SessionTTL, _defaultAuthSessionTTL)
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
		"SC4R_DEV_MODE":       "true",
		"SC4R_STORAGE_DRIVER": StorageDriverSQLite,
		"SC4R_SQLITE_PATH":    ":memory:",
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
			"SC4R_DEV_MODE":       "true",
			"SC4R_DEV_SCENARIO":   "happy",
			"SC4R_STORAGE_DRIVER": StorageDriverSQLite,
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
		"SC4R_DEV_MODE":       "true",
		"SC4R_DEV_SCENARIO":   "nope",
		"SC4R_STORAGE_DRIVER": StorageDriverSQLite,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid dev scenario") {
		t.Fatalf("Load() error = %v, want invalid dev scenario", err)
	}
}

func TestPrintConfigPathLoadsInDevModeWithoutCredentials(t *testing.T) {
	t.Parallel()

	cfg, err := Load(
		[]string{"--print-config", "--dev-mode", "--storage-driver", StorageDriverSQLite},
		map[string]string{},
	)
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

func TestLoad_MissingStorageDriver(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	delete(env, "SC4R_STORAGE_DRIVER")

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "invalid storage driver") {
		t.Fatalf("Load() error = %v, want invalid storage driver", err)
	}
}

func TestLoad_InvalidStorageDriver(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	env["SC4R_STORAGE_DRIVER"] = "memory"

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "invalid storage driver") {
		t.Fatalf("Load() error = %v, want invalid storage driver", err)
	}
}

func TestLoad_SQLiteFlags(t *testing.T) {
	t.Parallel()

	env := requiredEnv()
	cfg, err := Load(
		[]string{
			"--sqlite-path", "local.sqlite3",
			"--sqlite-wipe-on-start",
		},
		env,
	)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Storage.SQLite.Path != "local.sqlite3" {
		t.Fatalf("SQLite.Path = %q, want local.sqlite3", cfg.Storage.SQLite.Path)
	}

	if !cfg.Storage.SQLite.WipeOnStart {
		t.Fatal("SQLite.WipeOnStart = false, want true")
	}
}

func TestLoad_PostgresConfig(t *testing.T) {
	t.Parallel()

	env := requiredPostgresEnv()
	cfg, err := Load(nil, env)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Storage.Driver != StorageDriverPostgres {
		t.Fatalf("Storage.Driver = %q, want %q", cfg.Storage.Driver, StorageDriverPostgres)
	}

	if cfg.Storage.Postgres.Port != _defaultPostgresPort {
		t.Fatalf("Postgres.Port = %d, want %d", cfg.Storage.Postgres.Port, _defaultPostgresPort)
	}

	if cfg.Storage.Postgres.SSLMode != _defaultPostgresSSLMode {
		t.Fatalf("Postgres.SSLMode = %q, want %q", cfg.Storage.Postgres.SSLMode, _defaultPostgresSSLMode)
	}

	if !cfg.Storage.Postgres.MigrateOnStart {
		t.Fatal("Postgres.MigrateOnStart = false, want true")
	}
}

func TestLoad_PostgresMissingRequired(t *testing.T) {
	t.Parallel()

	env := requiredPostgresEnv()
	delete(env, "SC4R_POSTGRES_PASSWORD")

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "postgres password") {
		t.Fatalf("Load() error = %v, want postgres password error", err)
	}
}

func TestLoad_PostgresInvalidPort(t *testing.T) {
	t.Parallel()

	env := requiredPostgresEnv()
	env["SC4R_POSTGRES_PORT"] = "70000"

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "invalid postgres port") {
		t.Fatalf("Load() error = %v, want invalid postgres port", err)
	}
}

func TestLoad_PostgresInvalidSSLMode(t *testing.T) {
	t.Parallel()

	env := requiredPostgresEnv()
	env["SC4R_POSTGRES_SSL_MODE"] = "nope"

	_, err := Load(nil, env)
	if err == nil || !strings.Contains(err.Error(), "invalid postgres ssl mode") {
		t.Fatalf("Load() error = %v, want invalid ssl mode", err)
	}
}

func TestRedactedPostgresPassword(t *testing.T) {
	t.Parallel()

	cfg, err := Load(nil, requiredPostgresEnv())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	redacted := cfg.Redacted()
	if redacted.Storage.Postgres.Password != "****" {
		t.Fatalf("Postgres.Password = %q, want redacted", redacted.Storage.Postgres.Password)
	}
}

func requiredEnv() map[string]string {
	return map[string]string{
		"SC4R_SMARTCAR_CLIENT_ID":     "client-123456",
		"SC4R_SMARTCAR_CLIENT_SECRET": "secret-123456",
		"SC4R_SMARTCAR_USER_ID":       "user-123456",
		"SC4R_STORAGE_DRIVER":         StorageDriverSQLite,
	}
}

func requiredPostgresEnv() map[string]string {
	env := requiredEnv()
	env["SC4R_STORAGE_DRIVER"] = StorageDriverPostgres
	env["SC4R_POSTGRES_HOST"] = "db.example"
	env["SC4R_POSTGRES_USER"] = "toyota"
	env["SC4R_POSTGRES_PASSWORD"] = strings.Repeat("p", 12)
	env["SC4R_POSTGRES_DATABASE"] = "toyotaview"

	return env
}

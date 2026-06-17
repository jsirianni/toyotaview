package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	_defaultAddr               = "127.0.0.1:8080"
	_defaultAuthSessionTTL     = 12 * time.Hour
	_defaultDevScenario        = "happy"
	_defaultIAMBaseURL         = "https://iam.smartcar.com"
	_defaultPostgresPort       = 5432
	_defaultPostgresSSLMode    = "require"
	_defaultSQLitePath         = "./toyotaview.sqlite3"
	_defaultVehicleBaseURL     = "https://vehicle.api.smartcar.com/v3"
	_defaultUnitSystem         = "imperial"
	_defaultServiceName        = "smartcar-4runner"
	_defaultOTELServiceVersion = "dev"
)

const (
	StorageDriverPostgres = "postgres"
	StorageDriverSQLite   = "sqlite"
)

var _defaultSignalCodes = []string{
	"odometer-traveleddistance",
	"internalcombustionengine-fuellevel",
	"internalcombustionengine-amountremaining",
	"internalcombustionengine-range",
	"internalcombustionengine-oillife",
	"diagnostics-dtccount",
	"diagnostics-dtclist",
	"diagnostics-mil",
	"diagnostics-brakefluid",
	"diagnostics-oilpressure",
	"diagnostics-oiltemperature",
	"diagnostics-tirepressure",
	"service-isinservice",
	"service-records",
}

var (
	errMissingClientID     = errors.New("smartcar client id is required")
	errMissingClientSecret = errors.New("smartcar client secret is required")
	errMissingUserID       = errors.New("smartcar user id is required")
)

type Config struct {
	Addr              string         `json:"addr"`
	ReadHeaderTimeout time.Duration  `json:"readHeaderTimeout"`
	ReadTimeout       time.Duration  `json:"readTimeout"`
	WriteTimeout      time.Duration  `json:"writeTimeout"`
	IdleTimeout       time.Duration  `json:"idleTimeout"`
	ShutdownTimeout   time.Duration  `json:"shutdownTimeout"`
	Auth              AuthConfig     `json:"auth"`
	Dev               DevConfig      `json:"dev"`
	PrintConfig       bool           `json:"printConfig"`
	Smartcar          SmartcarConfig `json:"smartcar"`
	Storage           StorageConfig  `json:"storage"`
	Logging           LoggingConfig  `json:"logging"`
	OTEL              OTELConfig     `json:"otel"`
}

type AuthConfig struct {
	SessionTTL time.Duration `json:"sessionTTL"`
}

type DevConfig struct {
	Enabled  bool   `json:"enabled"`
	Scenario string `json:"scenario"`
}

type HelpError struct {
	Message string
}

func (e HelpError) Error() string {
	return e.Message
}

type SmartcarConfig struct {
	ClientID       string        `json:"clientID"`
	ClientSecret   string        `json:"clientSecret"`
	UserID         string        `json:"userID"`
	IAMBaseURL     string        `json:"iamBaseURL"`
	VehicleBaseURL string        `json:"vehicleBaseURL"`
	VehicleIDs     []string      `json:"vehicleIDs"`
	SignalCodes    []string      `json:"signalCodes"`
	UnitSystem     string        `json:"unitSystem"`
	Timeout        time.Duration `json:"timeout"`
	MaxRetries     int           `json:"maxRetries"`
}

type StorageConfig struct {
	Driver   string         `json:"driver"`
	SQLite   SQLiteConfig   `json:"sqlite"`
	Postgres PostgresConfig `json:"postgres"`
}

type SQLiteConfig struct {
	Path        string `json:"path"`
	WipeOnStart bool   `json:"wipeOnStart"`
}

type PostgresConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"user"`
	Password       string `json:"password"`
	Database       string `json:"database"`
	SSLMode        string `json:"sslMode"`
	MigrateOnStart bool   `json:"migrateOnStart"`
}

type LoggingConfig struct {
	File       string `json:"file"`
	Level      string `json:"level"`
	MaxSizeMB  int    `json:"maxSizeMB"`
	MaxBackups int    `json:"maxBackups"`
	MaxAgeDays int    `json:"maxAgeDays"`
	Compress   bool   `json:"compress"`
	AddStdout  bool   `json:"addStdout"`
}

type OTELConfig struct {
	Enabled        bool          `json:"enabled"`
	ServiceName    string        `json:"serviceName"`
	ServiceVersion string        `json:"serviceVersion"`
	Endpoint       string        `json:"endpoint"`
	Insecure       bool          `json:"insecure"`
	Timeout        time.Duration `json:"timeout"`
	SampleRatio    float64       `json:"sampleRatio"`
	CAFile         string        `json:"caFile"`
	ClientCertFile string        `json:"clientCertFile"`
	ClientKeyFile  string        `json:"clientKeyFile"`
	ServerName     string        `json:"serverName"`
}

func Load(args []string, env map[string]string) (Config, error) {
	clientIDDefault := envOrDefault(env, "SC4R_SMARTCAR_CLIENT_ID", "")
	clientSecretDefault := envOrDefault(env, "SC4R_SMARTCAR_CLIENT_SECRET", "")
	userIDDefault := envOrDefault(env, "SC4R_SMARTCAR_USER_ID", "")
	iamBaseURLDefault := envOrDefault(env, "SC4R_SMARTCAR_IAM_BASE_URL", _defaultIAMBaseURL)
	vehicleBaseURLDefault := envOrDefault(
		env,
		"SC4R_SMARTCAR_VEHICLE_BASE_URL",
		_defaultVehicleBaseURL,
	)
	vehicleIDsDefault := envOrDefault(env, "SC4R_SMARTCAR_VEHICLE_IDS", "")
	signalCodesDefault := envOrDefault(
		env,
		"SC4R_SMARTCAR_SIGNAL_CODES",
		strings.Join(_defaultSignalCodes, ","),
	)
	unitSystemDefault := envOrDefault(env, "SC4R_SMARTCAR_UNIT_SYSTEM", _defaultUnitSystem)

	smartcarTimeoutDefault, err := durationEnvOrDefault(
		env,
		"SC4R_SMARTCAR_TIMEOUT",
		20*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	smartcarRetriesDefault, err := intEnvOrDefault(env, "SC4R_SMARTCAR_MAX_RETRIES", 2)
	if err != nil {
		return Config{}, err
	}

	authSessionTTLDefault, err := durationEnvOrDefault(
		env,
		"SC4R_AUTH_SESSION_TTL",
		_defaultAuthSessionTTL,
	)
	if err != nil {
		return Config{}, err
	}

	addrDefault := envOrDefault(env, "SC4R_ADDR", _defaultAddr)
	devModeDefault, err := boolEnvOrDefault(env, "SC4R_DEV_MODE", false)
	if err != nil {
		return Config{}, err
	}

	devScenarioDefault := envOrDefault(env, "SC4R_DEV_SCENARIO", _defaultDevScenario)
	readHeaderTimeoutDefault, err := durationEnvOrDefault(
		env,
		"SC4R_READ_HEADER_TIMEOUT",
		5*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	readTimeoutDefault, err := durationEnvOrDefault(env, "SC4R_READ_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}

	writeTimeoutDefault, err := durationEnvOrDefault(env, "SC4R_WRITE_TIMEOUT", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	idleTimeoutDefault, err := durationEnvOrDefault(env, "SC4R_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}

	shutdownTimeoutDefault, err := durationEnvOrDefault(
		env,
		"SC4R_SHUTDOWN_TIMEOUT",
		10*time.Second,
	)
	if err != nil {
		return Config{}, err
	}

	logFileDefault := envOrDefault(env, "SC4R_LOG_FILE", "./smartcar-4runner.log")
	logLevelDefault := envOrDefault(env, "SC4R_LOG_LEVEL", "info")
	logMaxSizeDefault, err := intEnvOrDefault(env, "SC4R_LOG_MAX_SIZE_MB", 10)
	if err != nil {
		return Config{}, err
	}

	logMaxBackupsDefault, err := intEnvOrDefault(env, "SC4R_LOG_MAX_BACKUPS", 5)
	if err != nil {
		return Config{}, err
	}

	logMaxAgeDefault, err := intEnvOrDefault(env, "SC4R_LOG_MAX_AGE_DAYS", 30)
	if err != nil {
		return Config{}, err
	}

	logCompressDefault, err := boolEnvOrDefault(env, "SC4R_LOG_COMPRESS", true)
	if err != nil {
		return Config{}, err
	}

	logAddStdoutDefault, err := boolEnvOrDefault(env, "SC4R_LOG_ADD_STDOUT", false)
	if err != nil {
		return Config{}, err
	}

	storageDriverDefault := normalizeStorageDriver(envOrDefault(env, "SC4R_STORAGE_DRIVER", ""))
	sqlitePathDefault := envOrDefault(env, "SC4R_SQLITE_PATH", _defaultSQLitePath)
	sqliteWipeOnStartDefault, err := boolEnvOrDefault(
		env,
		"SC4R_SQLITE_WIPE_ON_START",
		false,
	)
	if err != nil {
		return Config{}, err
	}

	postgresHostDefault := envOrDefault(env, "SC4R_POSTGRES_HOST", "")
	postgresPortDefault, err := intEnvOrDefault(
		env,
		"SC4R_POSTGRES_PORT",
		_defaultPostgresPort,
	)
	if err != nil {
		return Config{}, err
	}

	postgresUserDefault := envOrDefault(env, "SC4R_POSTGRES_USER", "")
	postgresPasswordDefault := envOrDefault(env, "SC4R_POSTGRES_PASSWORD", "")
	postgresDatabaseDefault := envOrDefault(env, "SC4R_POSTGRES_DATABASE", "")
	postgresSSLModeDefault := normalizePostgresSSLMode(envOrDefault(
		env,
		"SC4R_POSTGRES_SSL_MODE",
		_defaultPostgresSSLMode,
	))
	postgresMigrateOnStartDefault, err := boolEnvOrDefault(
		env,
		"SC4R_POSTGRES_MIGRATE_ON_START",
		true,
	)
	if err != nil {
		return Config{}, err
	}

	otelEnabledDefault, err := boolEnvOrDefault(env, "SC4R_OTEL_ENABLED", false)
	if err != nil {
		return Config{}, err
	}

	otelServiceNameDefault := envOrDefault(env, "SC4R_OTEL_SERVICE_NAME", _defaultServiceName)
	otelServiceVersionDefault := envOrDefault(
		env,
		"SC4R_OTEL_SERVICE_VERSION",
		_defaultOTELServiceVersion,
	)
	otelEndpointDefault := envOrDefault(env, "SC4R_OTEL_ENDPOINT", "localhost:4318")

	otelInsecureDefault, err := boolEnvOrDefault(env, "SC4R_OTEL_INSECURE", true)
	if err != nil {
		return Config{}, err
	}

	otelTimeoutDefault, err := durationEnvOrDefault(env, "SC4R_OTEL_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	otelSampleRatioDefault, err := floatEnvOrDefault(env, "SC4R_OTEL_SAMPLE_RATIO", 1.0)
	if err != nil {
		return Config{}, err
	}

	otelCAFileDefault := envOrDefault(env, "SC4R_OTEL_CA_FILE", "")
	otelClientCertDefault := envOrDefault(env, "SC4R_OTEL_CLIENT_CERT_FILE", "")
	otelClientKeyDefault := envOrDefault(env, "SC4R_OTEL_CLIENT_KEY_FILE", "")
	otelServerNameDefault := envOrDefault(env, "SC4R_OTEL_SERVER_NAME", "")

	fs := flag.NewFlagSet("smartcar-4runner", flag.ContinueOnError)
	var usage strings.Builder
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		fmt.Fprintf(&usage, "Usage of %s:\n", fs.Name())
		fs.SetOutput(&usage)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}

	cfg := Config{}
	fs.DurationVar(
		&cfg.Auth.SessionTTL,
		"auth-session-ttl",
		authSessionTTLDefault,
		"Authenticated browser session lifetime",
	)

	fs.StringVar(&cfg.Smartcar.ClientID, "smartcar-client-id", clientIDDefault, "Smartcar client id")
	fs.StringVar(
		&cfg.Smartcar.ClientSecret,
		"smartcar-client-secret",
		clientSecretDefault,
		"Smartcar client secret",
	)
	fs.StringVar(&cfg.Smartcar.UserID, "smartcar-user-id", userIDDefault, "Smartcar user id")
	fs.StringVar(
		&cfg.Smartcar.IAMBaseURL,
		"smartcar-iam-base-url",
		iamBaseURLDefault,
		"Smartcar IAM base URL",
	)
	fs.StringVar(
		&cfg.Smartcar.VehicleBaseURL,
		"smartcar-vehicle-base-url",
		vehicleBaseURLDefault,
		"Smartcar vehicle API base URL",
	)
	vehicleIDs := fs.String(
		"smartcar-vehicle-ids",
		vehicleIDsDefault,
		"Comma-separated vehicle ID allowlist",
	)
	signalCodes := fs.String(
		"smartcar-signal-codes",
		signalCodesDefault,
		"Comma-separated signal code allowlist",
	)
	fs.StringVar(
		&cfg.Smartcar.UnitSystem,
		"smartcar-unit-system",
		unitSystemDefault,
		"Smartcar unit system",
	)
	fs.DurationVar(
		&cfg.Smartcar.Timeout,
		"smartcar-timeout",
		smartcarTimeoutDefault,
		"Smartcar client timeout",
	)
	fs.IntVar(
		&cfg.Smartcar.MaxRetries,
		"smartcar-max-retries",
		smartcarRetriesDefault,
		"Smartcar transient retry count",
	)

	fs.StringVar(&cfg.Addr, "addr", addrDefault, "HTTP listen address")
	fs.BoolVar(
		&cfg.Dev.Enabled,
		"dev-mode",
		devModeDefault,
		"Enable mocked Smartcar backend for local development",
	)
	fs.StringVar(
		&cfg.Dev.Scenario,
		"dev-scenario",
		devScenarioDefault,
		"Mocked Smartcar scenario for dev mode",
	)
	fs.DurationVar(
		&cfg.ReadHeaderTimeout,
		"read-header-timeout",
		readHeaderTimeoutDefault,
		"HTTP read header timeout",
	)
	fs.DurationVar(&cfg.ReadTimeout, "read-timeout", readTimeoutDefault, "HTTP read timeout")
	fs.DurationVar(&cfg.WriteTimeout, "write-timeout", writeTimeoutDefault, "HTTP write timeout")
	fs.DurationVar(&cfg.IdleTimeout, "idle-timeout", idleTimeoutDefault, "HTTP idle timeout")
	fs.DurationVar(
		&cfg.ShutdownTimeout,
		"shutdown-timeout",
		shutdownTimeoutDefault,
		"Graceful shutdown timeout",
	)

	fs.StringVar(&cfg.Logging.File, "log-file", logFileDefault, "Log file path")
	fs.StringVar(&cfg.Logging.Level, "log-level", logLevelDefault, "Log level")
	fs.IntVar(&cfg.Logging.MaxSizeMB, "log-max-size-mb", logMaxSizeDefault, "Log max size")
	fs.IntVar(
		&cfg.Logging.MaxBackups,
		"log-max-backups",
		logMaxBackupsDefault,
		"Log max backups",
	)
	fs.IntVar(
		&cfg.Logging.MaxAgeDays,
		"log-max-age-days",
		logMaxAgeDefault,
		"Log max age in days",
	)
	fs.BoolVar(&cfg.Logging.Compress, "log-compress", logCompressDefault, "Compress old logs")
	fs.BoolVar(
		&cfg.Logging.AddStdout,
		"log-add-stdout",
		logAddStdoutDefault,
		"Duplicate logs to stdout",
	)

	fs.StringVar(
		&cfg.Storage.Driver,
		"storage-driver",
		storageDriverDefault,
		"Durable storage driver: sqlite or postgres",
	)
	fs.StringVar(
		&cfg.Storage.SQLite.Path,
		"sqlite-path",
		sqlitePathDefault,
		"SQLite database path for local development",
	)
	fs.BoolVar(
		&cfg.Storage.SQLite.WipeOnStart,
		"sqlite-wipe-on-start",
		sqliteWipeOnStartDefault,
		"Wipe the SQLite database before startup",
	)
	fs.StringVar(
		&cfg.Storage.Postgres.Host,
		"postgres-host",
		postgresHostDefault,
		"Postgres host",
	)
	fs.IntVar(
		&cfg.Storage.Postgres.Port,
		"postgres-port",
		postgresPortDefault,
		"Postgres port",
	)
	fs.StringVar(
		&cfg.Storage.Postgres.User,
		"postgres-user",
		postgresUserDefault,
		"Postgres user",
	)
	fs.StringVar(
		&cfg.Storage.Postgres.Password,
		"postgres-password",
		postgresPasswordDefault,
		"Postgres password",
	)
	fs.StringVar(
		&cfg.Storage.Postgres.Database,
		"postgres-database",
		postgresDatabaseDefault,
		"Postgres database",
	)
	fs.StringVar(
		&cfg.Storage.Postgres.SSLMode,
		"postgres-ssl-mode",
		postgresSSLModeDefault,
		"Postgres sslmode",
	)
	fs.BoolVar(
		&cfg.Storage.Postgres.MigrateOnStart,
		"postgres-migrate-on-start",
		postgresMigrateOnStartDefault,
		"Run Postgres migrations during startup",
	)

	fs.BoolVar(&cfg.OTEL.Enabled, "otel-enabled", otelEnabledDefault, "Enable OTEL")
	fs.StringVar(
		&cfg.OTEL.ServiceName,
		"otel-service-name",
		otelServiceNameDefault,
		"OTEL service name",
	)
	fs.StringVar(
		&cfg.OTEL.ServiceVersion,
		"otel-service-version",
		otelServiceVersionDefault,
		"OTEL service version",
	)
	fs.StringVar(&cfg.OTEL.Endpoint, "otel-endpoint", otelEndpointDefault, "OTEL endpoint")
	fs.BoolVar(&cfg.OTEL.Insecure, "otel-insecure", otelInsecureDefault, "Use insecure OTEL")
	fs.DurationVar(&cfg.OTEL.Timeout, "otel-timeout", otelTimeoutDefault, "OTEL timeout")
	fs.Float64Var(
		&cfg.OTEL.SampleRatio,
		"otel-sample-ratio",
		otelSampleRatioDefault,
		"OTEL sample ratio",
	)
	fs.StringVar(&cfg.OTEL.CAFile, "otel-ca-file", otelCAFileDefault, "OTEL CA bundle")
	fs.StringVar(
		&cfg.OTEL.ClientCertFile,
		"otel-client-cert-file",
		otelClientCertDefault,
		"OTEL client cert file",
	)
	fs.StringVar(
		&cfg.OTEL.ClientKeyFile,
		"otel-client-key-file",
		otelClientKeyDefault,
		"OTEL client key file",
	)
	fs.StringVar(
		&cfg.OTEL.ServerName,
		"otel-server-name",
		otelServerNameDefault,
		"OTEL TLS server name",
	)
	fs.BoolVar(&cfg.PrintConfig, "print-config", false, "Print redacted config and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, HelpError{Message: usage.String()}
		}

		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	cfg.Smartcar.VehicleIDs = parseCSV(*vehicleIDs)
	cfg.Smartcar.SignalCodes = parseCSV(*signalCodes)
	cfg.Dev.Scenario = normalizeDevScenario(cfg.Dev.Scenario)
	cfg.Storage.Driver = normalizeStorageDriver(cfg.Storage.Driver)
	cfg.Storage.Postgres.SSLMode = normalizePostgresSSLMode(cfg.Storage.Postgres.SSLMode)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Auth.SessionTTL <= 0 {
		return errors.New("auth session ttl must be greater than zero")
	}

	if c.Dev.Enabled {
		if err := validateDevScenario(c.Dev.Scenario); err != nil {
			return err
		}
	} else {
		if c.Smartcar.ClientID == "" {
			return errMissingClientID
		}

		if c.Smartcar.ClientSecret == "" {
			return errMissingClientSecret
		}

		if c.Smartcar.UserID == "" {
			return errMissingUserID
		}
	}

	if _, err := parseURL(c.Smartcar.IAMBaseURL); err != nil {
		return fmt.Errorf("validate smartcar iam base url: %w", err)
	}

	if _, err := parseURL(c.Smartcar.VehicleBaseURL); err != nil {
		return fmt.Errorf("validate smartcar vehicle base url: %w", err)
	}

	if len(c.Smartcar.SignalCodes) == 0 {
		return errors.New("smartcar signal codes must not be empty")
	}

	switch c.Smartcar.UnitSystem {
	case "imperial", "metric":
	default:
		return fmt.Errorf("invalid smartcar unit system %q", c.Smartcar.UnitSystem)
	}

	if c.Smartcar.Timeout <= 0 {
		return errors.New("smartcar timeout must be greater than zero")
	}

	if c.Smartcar.MaxRetries < 0 {
		return errors.New("smartcar max retries must be zero or greater")
	}

	if c.Addr == "" {
		return errors.New("addr is required")
	}

	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return fmt.Errorf("invalid addr %q: %w", c.Addr, err)
	}

	if c.ReadHeaderTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 {
		return errors.New("server timeouts must be greater than zero")
	}

	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be greater than zero")
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log level %q", c.Logging.Level)
	}

	if c.Logging.File == "" {
		return errors.New("log file is required")
	}

	if c.Logging.MaxSizeMB <= 0 || c.Logging.MaxBackups < 0 || c.Logging.MaxAgeDays < 0 {
		return errors.New("invalid log rotation settings")
	}

	switch c.Storage.Driver {
	case StorageDriverSQLite:
		if strings.TrimSpace(c.Storage.SQLite.Path) == "" {
			return errors.New("sqlite path is required")
		}
	case StorageDriverPostgres:
		if strings.TrimSpace(c.Storage.Postgres.Host) == "" {
			return errors.New("postgres host is required")
		}

		if c.Storage.Postgres.Port <= 0 || c.Storage.Postgres.Port > 65535 {
			return fmt.Errorf("invalid postgres port %d", c.Storage.Postgres.Port)
		}

		if strings.TrimSpace(c.Storage.Postgres.User) == "" {
			return errors.New("postgres user is required")
		}

		if c.Storage.Postgres.Password == "" {
			return errors.New("postgres password is required")
		}

		if strings.TrimSpace(c.Storage.Postgres.Database) == "" {
			return errors.New("postgres database is required")
		}

		if err := validatePostgresSSLMode(c.Storage.Postgres.SSLMode); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid storage driver %q", c.Storage.Driver)
	}

	if c.OTEL.ServiceName == "" {
		return errors.New("otel service name is required")
	}

	if c.OTEL.ServiceVersion == "" {
		return errors.New("otel service version is required")
	}

	if c.OTEL.Timeout <= 0 {
		return errors.New("otel timeout must be greater than zero")
	}

	if c.OTEL.SampleRatio < 0 || c.OTEL.SampleRatio > 1 {
		return fmt.Errorf("otel sample ratio must be between 0 and 1, got %v", c.OTEL.SampleRatio)
	}

	if (c.OTEL.ClientCertFile == "") != (c.OTEL.ClientKeyFile == "") {
		return errors.New("otel client cert and key must both be set")
	}

	return nil
}

func (c Config) Redacted() Config {
	redacted := c
	redacted.Smartcar.ClientID = redactValue(redacted.Smartcar.ClientID, 4)
	redacted.Smartcar.ClientSecret = redactValue(redacted.Smartcar.ClientSecret, 0)
	redacted.Smartcar.UserID = redactValue(redacted.Smartcar.UserID, 6)
	redacted.Storage.Postgres.Password = redactValue(redacted.Storage.Postgres.Password, 0)

	return redacted
}

func (c Config) RedactedJSON() ([]byte, error) {
	payload, err := json.MarshalIndent(c.Redacted(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal redacted config: %w", err)
	}

	return payload, nil
}

func (c Config) IsLoopback() bool {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return false
	}

	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}

func DefaultSignalCodes() []string {
	signalCodes := make([]string, len(_defaultSignalCodes))
	copy(signalCodes, _defaultSignalCodes)

	return signalCodes
}

func normalizeDevScenario(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return _defaultDevScenario
	}

	return value
}

func normalizeStorageDriver(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizePostgresSSLMode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validatePostgresSSLMode(value string) error {
	switch value {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("invalid postgres ssl mode %q", value)
	}
}

func validateDevScenario(value string) error {
	switch value {
	case "happy", "partial", "empty", "failure":
		return nil
	default:
		return fmt.Errorf("invalid dev scenario %q", value)
	}
}

func parseCSV(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		values = append(values, part)
	}

	return values
}

func envOrDefault(env map[string]string, key string, fallback string) string {
	if value, ok := env[key]; ok {
		return strings.TrimSpace(value)
	}

	return fallback
}

func durationEnvOrDefault(
	env map[string]string,
	key string,
	fallback time.Duration,
) (time.Duration, error) {
	value := envOrDefault(env, key, "")
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func intEnvOrDefault(env map[string]string, key string, fallback int) (int, error) {
	value := envOrDefault(env, key, "")
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func boolEnvOrDefault(env map[string]string, key string, fallback bool) (bool, error) {
	value := envOrDefault(env, key, "")
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func floatEnvOrDefault(env map[string]string, key string, fallback float64) (float64, error) {
	value := envOrDefault(env, key, "")
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}

	return parsed, nil
}

func parseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, err
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("must be an absolute URL")
	}

	return parsed, nil
}

func redactValue(value string, keepSuffix int) string {
	if value == "" {
		return ""
	}

	if keepSuffix <= 0 || len(value) <= keepSuffix {
		return "****"
	}

	return strings.Repeat("*", len(value)-keepSuffix) + value[len(value)-keepSuffix:]
}

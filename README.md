# smartcar-4runner

`smartcar-4runner` is a local Go web app that renders Toyota vehicle data from Smartcar on a simple server-side HTML dashboard. The page shows cached data on `GET` requests and only talks to Smartcar when a user clicks a refresh button in the browser.

## Security note

This app gates the browser dashboard behind local username/password login. Smartcar access is still configured globally for the running instance, so all authenticated users see the same configured vehicle dashboard.

## What it does

- Lists Toyota vehicles connected to the configured Smartcar user.
- Shows vehicle attributes such as make, model, year, powertrain, and mode.
- Retrieves a configurable set of Smartcar signals on demand.
- Preserves the latest successful snapshots in memory between page loads.
- Surfaces refresh failures in both the browser UI and structured logs.

## Smartcar prerequisites

You need:

- A Smartcar application configured for the v3 Vehicle API.
- A Smartcar `client_id` and `client_secret`.
- A Smartcar `user_id` captured outside this app from Smartcar Connect or another approved flow.

Smartcar API authentication uses the client-credentials flow. This app never stores Smartcar credentials or access tokens on disk.

## Local run

```bash
export SC4R_SMARTCAR_CLIENT_ID="..."
export SC4R_SMARTCAR_CLIENT_SECRET="..."
export SC4R_SMARTCAR_USER_ID="..."
export SC4R_STORAGE_DRIVER="sqlite"
export SC4R_LOG_FILE="./smartcar-4runner.log"
go run ./cmd/smartcar-4runner
```

Then open [http://127.0.0.1:8080/signup](http://127.0.0.1:8080/signup) to create a local account.

To inspect the effective configuration without making any network calls:

```bash
go run ./cmd/smartcar-4runner --print-config
```

## Dev mode

Dev mode runs the full web app against a built-in mocked Smartcar backend. You do not need real Smartcar credentials, a `user_id`, or connected vehicles.

```bash
go run ./cmd/smartcar-4runner --dev-mode --storage-driver sqlite
```

Create a local account at `/signup`. The dashboard still starts empty on `GET /`; use the browser refresh actions to populate mocked vehicle data, just like the real app.

You can switch between built-in mock scenarios:

```bash
go run ./cmd/smartcar-4runner --dev-mode --storage-driver sqlite --dev-scenario happy
go run ./cmd/smartcar-4runner --dev-mode --storage-driver sqlite --dev-scenario partial
go run ./cmd/smartcar-4runner --dev-mode --storage-driver sqlite --dev-scenario empty
go run ./cmd/smartcar-4runner --dev-mode --storage-driver sqlite --dev-scenario failure
```

You can also scope the mocked dashboard to a single built-in vehicle:

```bash
SC4R_DEV_MODE=true SC4R_STORAGE_DRIVER=sqlite SC4R_SMARTCAR_VEHICLE_IDS=dev-4runner go run ./cmd/smartcar-4runner
```

Built-in mocked vehicle IDs:

- `dev-4runner`
- `dev-tacoma`

## Container run

```bash
docker run --rm \
  -p 127.0.0.1:8080:8080 \
  -e SC4R_ADDR=0.0.0.0:8080 \
  -e SC4R_SMARTCAR_CLIENT_ID="..." \
  -e SC4R_SMARTCAR_CLIENT_SECRET="..." \
  -e SC4R_SMARTCAR_USER_ID="..." \
  -e SC4R_STORAGE_DRIVER=sqlite \
  -e SC4R_SQLITE_PATH=/data/toyotaview.sqlite3 \
  -e SC4R_LOG_FILE=/data/smartcar-4runner.log \
  -v smartcar-4runner-data:/data \
  ghcr.io/OWNER/smartcar-4runner:latest
```

## Refresh behavior

- `GET /` shows only cached data.
- `POST /refresh` refreshes all selected vehicles.
- `POST /vehicles/{vehicleID}/refresh` refreshes one vehicle.
- Unsupported or unavailable signals are shown per signal and do not fail the entire dashboard refresh.

## Signal caveat

Smartcar signal availability varies by OEM support, permissions, region, subscription status, and vehicle state. Toyota vehicles may not expose every configured signal at all times.

## Core configuration

Flags override environment variables, and environment variables override defaults.

### Required

| Flag | Environment variable |
|---|---|
| `--storage-driver` | `SC4R_STORAGE_DRIVER` |
| `--smartcar-client-id` | `SC4R_SMARTCAR_CLIENT_ID` |
| `--smartcar-client-secret` | `SC4R_SMARTCAR_CLIENT_SECRET` |
| `--smartcar-user-id` | `SC4R_SMARTCAR_USER_ID` |

Smartcar credentials are not required when `--dev-mode` is enabled.

### Common optional settings

| Flag | Environment variable | Default |
|---|---|---|
| `--addr` | `SC4R_ADDR` | `127.0.0.1:8080` |
| `--dev-mode` | `SC4R_DEV_MODE` | `false` |
| `--dev-scenario` | `SC4R_DEV_SCENARIO` | `happy` |
| `--smartcar-vehicle-ids` | `SC4R_SMARTCAR_VEHICLE_IDS` | empty |
| `--smartcar-signal-codes` | `SC4R_SMARTCAR_SIGNAL_CODES` | built-in defaults |
| `--smartcar-unit-system` | `SC4R_SMARTCAR_UNIT_SYSTEM` | `imperial` |
| `--smartcar-timeout` | `SC4R_SMARTCAR_TIMEOUT` | `20s` |
| `--log-file` | `SC4R_LOG_FILE` | `./smartcar-4runner.log` |
| `--log-level` | `SC4R_LOG_LEVEL` | `info` |
| `--auth-session-ttl` | `SC4R_AUTH_SESSION_TTL` | `12h` |
| `--sqlite-path` | `SC4R_SQLITE_PATH` | `./toyotaview.sqlite3` |
| `--sqlite-wipe-on-start` | `SC4R_SQLITE_WIPE_ON_START` | `false` |
| `--otel-enabled` | `SC4R_OTEL_ENABLED` | `false` |
| `--otel-endpoint` | `SC4R_OTEL_ENDPOINT` | `localhost:4318` |

For production Postgres storage, set `--storage-driver postgres` plus `--postgres-host`, `--postgres-user`, `--postgres-password`, and `--postgres-database`. Postgres migrations run on startup by default.

Run `go run ./cmd/smartcar-4runner --help` for the full flag list.

## Logging

- Logs are JSON.
- File rotation uses lumberjack.
- Secrets are redacted in startup config logging.
- Smartcar access tokens and client secrets are not logged.

## OpenTelemetry

OTEL is optional. When enabled, the app exports traces and metrics over OTLP/HTTP.

Example:

```bash
go run ./cmd/smartcar-4runner \
  --otel-enabled \
  --otel-endpoint localhost:4318 \
  --otel-insecure
```

## Development commands

```bash
make fmt
make test
make vet
make lint
make gosec
make snapshot
```

The `Makefile` and `scripts/` directory provide the same commands in shortcut form.

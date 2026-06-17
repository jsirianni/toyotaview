package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/firefoxx04/toyotaview/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib" // Register the pgx database/sql driver.
)

//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

var _ Store = (*Postgres)(nil)

const postgresMigrationLockID = 949411716

type Postgres struct {
	*sqlStore
}

func NewPostgres(cfg config.PostgresConfig) (*Postgres, error) {
	db, err := sql.Open("pgx", PostgresDataSourceName(cfg))
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	return &Postgres{
		sqlStore: &sqlStore{
			db:      db,
			dialect: sqlDialectPostgres,
		},
	}, nil
}

func PostgresDataSourceName(cfg config.PostgresConfig) string {
	values := url.Values{}
	values.Set("sslmode", cfg.SSLMode)

	postgresURL := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     "/" + cfg.Database,
		RawQuery: values.Encode(),
	}

	return postgresURL.String()
}

func (p *Postgres) Migrate(ctx context.Context) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin postgres migration: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(
		ctx,
		`SELECT pg_advisory_xact_lock($1)`,
		postgresMigrationLockID,
	); err != nil {
		return fmt.Errorf("lock postgres migrations: %w", err)
	}

	if _, err := tx.ExecContext(ctx, postgresMigrationTableSQL); err != nil {
		return fmt.Errorf("ensure postgres migration table: %w", err)
	}

	migrations, err := postgresMigrationFiles()
	if err != nil {
		return err
	}

	for _, migration := range migrations {
		applied, err := postgresMigrationApplied(ctx, tx, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("apply postgres migration %s: %w", migration.version, err)
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`,
			migration.version,
		); err != nil {
			return fmt.Errorf("record postgres migration %s: %w", migration.version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgres migrations: %w", err)
	}

	return nil
}

type postgresMigration struct {
	version string
	sql     string
}

func postgresMigrationFiles() ([]postgresMigration, error) {
	entries, err := fs.ReadDir(postgresMigrations, "migrations/postgres")
	if err != nil {
		return nil, fmt.Errorf("read postgres migrations: %w", err)
	}

	migrations := make([]postgresMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		path := "migrations/postgres/" + entry.Name()
		payload, err := postgresMigrations.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read postgres migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, postgresMigration{
			version: strings.TrimSuffix(entry.Name(), ".sql"),
			sql:     string(payload),
		})
	}

	return migrations, nil
}

func postgresMigrationApplied(ctx context.Context, tx *sql.Tx, version string) (bool, error) {
	var applied bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
		version,
	).Scan(&applied); err != nil {
		return false, fmt.Errorf("check postgres migration %s: %w", version, err)
	}

	return applied, nil
}

const postgresMigrationTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

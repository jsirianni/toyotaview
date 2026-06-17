package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/firefoxx04/toyotaview/internal/config"
	_ "modernc.org/sqlite" // Register the pure-Go SQLite database/sql driver.
)

var _ Store = (*SQLite)(nil)

type SQLite struct {
	*sqlStore
}

func NewSQLite(cfg config.SQLiteConfig) (*SQLite, error) {
	if cfg.WipeOnStart && isSQLiteFilesystemPath(cfg.Path) {
		if err := os.Remove(cfg.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("wipe sqlite database: %w", err)
		}
	}

	if err := ensureSQLiteDir(cfg.Path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	store := &SQLite{
		sqlStore: &sqlStore{
			db:      db,
			dialect: sqlDialectSQLite,
		},
	}
	if err := store.configure(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLite) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, sqliteSchema); err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}

	return nil
}

func (s *SQLite) configure(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}

	return nil
}

func ensureSQLiteDir(path string) error {
	if !isSQLiteFilesystemPath(path) {
		return nil
	}

	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create sqlite directory: %w", err)
	}

	return nil
}

func isSQLiteFilesystemPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	return trimmed != "" && trimmed != ":memory:" && !strings.HasPrefix(trimmed, "file:")
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions(expires_at);
`

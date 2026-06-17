package storage

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
)

func TestSQLitePingAndAuthData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newSQLiteTestStore(t, filepath.Join(t.TempDir(), "toyotaview.sqlite3"), false)

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	user, err := store.CreateUser(ctx, CreateUserParams{
		Username:     "driver",
		PasswordHash: "bcrypt-hash",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if user.ID == 0 {
		t.Fatal("User.ID = 0, want generated id")
	}

	found, err := store.FindUserByUsername(ctx, "driver")
	if err != nil {
		t.Fatalf("FindUserByUsername() error = %v", err)
	}

	if found.PasswordHash != "bcrypt-hash" {
		t.Fatalf("PasswordHash = %q, want bcrypt-hash", found.PasswordHash)
	}

	session, err := store.CreateSession(ctx, CreateSessionParams{
		TokenHash: "token-hash",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if session.TokenHash != "token-hash" {
		t.Fatalf("TokenHash = %q, want token-hash", session.TokenHash)
	}

	foundSession, err := store.FindSessionByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash() error = %v", err)
	}

	if foundSession.UserID != user.ID {
		t.Fatalf("Session.UserID = %d, want %d", foundSession.UserID, user.ID)
	}

	if err := store.DeleteSession(ctx, "token-hash"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	_, err = store.FindSessionByTokenHash(ctx, "token-hash")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("FindSessionByTokenHash() error = %v, want ErrSessionNotFound", err)
	}
}

func TestSQLiteRejectsDuplicateUsername(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newSQLiteTestStore(t, filepath.Join(t.TempDir(), "toyotaview.sqlite3"), false)

	_, err := store.CreateUser(ctx, CreateUserParams{
		Username:     "driver",
		PasswordHash: "one",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	_, err = store.CreateUser(ctx, CreateUserParams{
		Username:     "driver",
		PasswordHash: "two",
	})
	if !errors.Is(err, ErrDuplicateUser) {
		t.Fatalf("CreateUser() error = %v, want ErrDuplicateUser", err)
	}
}

func TestSQLiteWipeOnStart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "toyotaview.sqlite3")
	store := newSQLiteTestStore(t, path, false)

	_, err := store.CreateUser(ctx, CreateUserParams{
		Username:     "driver",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store = newSQLiteTestStore(t, path, true)
	_, err = store.FindUserByUsername(ctx, "driver")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("FindUserByUsername() error = %v, want ErrUserNotFound", err)
	}
}

func TestSQLiteExpiredSessionRejectedAndDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newSQLiteTestStore(t, filepath.Join(t.TempDir(), "toyotaview.sqlite3"), false)

	user, err := store.CreateUser(ctx, CreateUserParams{
		Username:     "driver",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	_, err = store.CreateSession(ctx, CreateSessionParams{
		TokenHash: "expired",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = store.FindSessionByTokenHash(ctx, "expired")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("FindSessionByTokenHash() error = %v, want ErrSessionNotFound", err)
	}

	if err := store.DeleteExpiredSessions(ctx); err != nil {
		t.Fatalf("DeleteExpiredSessions() error = %v", err)
	}
}

func TestPostgresDataSourceName(t *testing.T) {
	t.Parallel()

	dsn := PostgresDataSourceName(config.PostgresConfig{
		Host:     "db.example",
		Port:     5432,
		User:     "toyota",
		Password: strings.Repeat("p", 12),
		Database: "toyotaview",
		SSLMode:  "verify-full",
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	if parsed.Scheme != "postgres" {
		t.Fatalf("Scheme = %q, want postgres", parsed.Scheme)
	}

	if parsed.Host != "db.example:5432" {
		t.Fatalf("Host = %q, want db.example:5432", parsed.Host)
	}

	if parsed.Path != "/toyotaview" {
		t.Fatalf("Path = %q, want /toyotaview", parsed.Path)
	}

	if parsed.Query().Get("sslmode") != "verify-full" {
		t.Fatalf("sslmode = %q, want verify-full", parsed.Query().Get("sslmode"))
	}
}

func TestPostgresMigrationsIncludeAuthSchema(t *testing.T) {
	t.Parallel()

	migrations, err := postgresMigrationFiles()
	if err != nil {
		t.Fatalf("postgresMigrationFiles() error = %v", err)
	}

	if len(migrations) == 0 {
		t.Fatal("postgresMigrationFiles() returned no migrations")
	}

	sql := migrations[0].sql
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS users",
		"CREATE TABLE IF NOT EXISTS sessions",
		"REFERENCES users(id) ON DELETE CASCADE",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration sql missing %q", want)
		}
	}
}

func newSQLiteTestStore(t *testing.T, path string, wipe bool) *SQLite {
	t.Helper()

	store, err := NewSQLite(config.SQLiteConfig{
		Path:        path,
		WipeOnStart: wipe,
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}

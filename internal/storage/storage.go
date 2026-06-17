package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
)

var (
	ErrDuplicateUser   = errors.New("user already exists")
	ErrSessionNotFound = errors.New("session not found")
	ErrUserNotFound    = errors.New("user not found")
)

type Store interface {
	Ping(ctx context.Context) error
	Migrate(ctx context.Context) error
	Close() error
	UserStore
	SessionStore
}

type UserStore interface {
	CreateUser(ctx context.Context, params CreateUserParams) (User, error)
	FindUserByUsername(ctx context.Context, username string) (User, error)
}

type SessionStore interface {
	CreateSession(ctx context.Context, params CreateSessionParams) (Session, error)
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) error
}

type CreateUserParams struct {
	Username     string
	PasswordHash string
}

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type CreateSessionParams struct {
	TokenHash string
	UserID    int64
	ExpiresAt time.Time
}

type Session struct {
	TokenHash string
	UserID    int64
	ExpiresAt time.Time
	CreatedAt time.Time
}

type sqlDialect int

const (
	sqlDialectPostgres sqlDialect = iota + 1
	sqlDialectSQLite
)

type sqlStore struct {
	db      *sql.DB
	dialect sqlDialect
}

type scanner interface {
	Scan(dest ...any) error
}

func Open(cfg config.StorageConfig) (Store, error) {
	switch cfg.Driver {
	case config.StorageDriverSQLite:
		return NewSQLite(cfg.SQLite)
	case config.StorageDriverPostgres:
		return NewPostgres(cfg.Postgres)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Driver)
	}
}

func (s *sqlStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}

func (s *sqlStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

func (s *sqlStore) CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
	createdAt := time.Now().UTC().Unix()
	row := s.db.QueryRowContext(
		ctx,
		s.createUserSQL(),
		params.Username,
		params.PasswordHash,
		createdAt,
	)

	user, err := scanUser(row)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrDuplicateUser
		}

		return User{}, fmt.Errorf("create user: %w", err)
	}

	return user, nil
}

func (s *sqlStore) FindUserByUsername(ctx context.Context, username string) (User, error) {
	row := s.db.QueryRowContext(ctx, s.findUserByUsernameSQL(), username)
	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}

		return User{}, fmt.Errorf("find user by username: %w", err)
	}

	return user, nil
}

func (s *sqlStore) CreateSession(
	ctx context.Context,
	params CreateSessionParams,
) (Session, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(
		ctx,
		s.createSessionSQL(),
		params.TokenHash,
		params.UserID,
		params.ExpiresAt.UTC().Unix(),
		now.Unix(),
	)

	session, err := scanSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	return session, nil
}

func (s *sqlStore) FindSessionByTokenHash(
	ctx context.Context,
	tokenHash string,
) (Session, error) {
	row := s.db.QueryRowContext(
		ctx,
		s.findSessionByTokenHashSQL(),
		tokenHash,
		time.Now().UTC().Unix(),
	)

	session, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrSessionNotFound
		}

		return Session{}, fmt.Errorf("find session by token hash: %w", err)
	}

	return session, nil
}

func (s *sqlStore) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.db.ExecContext(ctx, s.deleteSessionSQL(), tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

func (s *sqlStore) DeleteExpiredSessions(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, s.deleteExpiredSessionsSQL(), time.Now().UTC().Unix()); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}

	return nil
}

func (s *sqlStore) createUserSQL() string {
	switch s.dialect {
	case sqlDialectPostgres:
		return `INSERT INTO users (username, password_hash, created_at)
VALUES ($1, $2, to_timestamp($3))
RETURNING id, username, password_hash, EXTRACT(EPOCH FROM created_at)::bigint`
	default:
		return `INSERT INTO users (username, password_hash, created_at)
VALUES (?, ?, ?)
RETURNING id, username, password_hash, created_at`
	}
}

func (s *sqlStore) findUserByUsernameSQL() string {
	switch s.dialect {
	case sqlDialectPostgres:
		return `SELECT id, username, password_hash, EXTRACT(EPOCH FROM created_at)::bigint
FROM users
WHERE username = $1`
	default:
		return `SELECT id, username, password_hash, created_at
FROM users
WHERE username = ?`
	}
}

func (s *sqlStore) createSessionSQL() string {
	switch s.dialect {
	case sqlDialectPostgres:
		return `INSERT INTO sessions (token_hash, user_id, expires_at, created_at)
VALUES ($1, $2, to_timestamp($3), to_timestamp($4))
RETURNING token_hash, user_id, EXTRACT(EPOCH FROM expires_at)::bigint, EXTRACT(EPOCH FROM created_at)::bigint`
	default:
		return `INSERT INTO sessions (token_hash, user_id, expires_at, created_at)
VALUES (?, ?, ?, ?)
RETURNING token_hash, user_id, expires_at, created_at`
	}
}

func (s *sqlStore) findSessionByTokenHashSQL() string {
	switch s.dialect {
	case sqlDialectPostgres:
		return `SELECT token_hash, user_id, EXTRACT(EPOCH FROM expires_at)::bigint, EXTRACT(EPOCH FROM created_at)::bigint
FROM sessions
WHERE token_hash = $1
  AND expires_at > to_timestamp($2)`
	default:
		return `SELECT token_hash, user_id, expires_at, created_at
FROM sessions
WHERE token_hash = ?
  AND expires_at > ?`
	}
}

func (s *sqlStore) deleteSessionSQL() string {
	switch s.dialect {
	case sqlDialectPostgres:
		return `DELETE FROM sessions WHERE token_hash = $1`
	default:
		return `DELETE FROM sessions WHERE token_hash = ?`
	}
}

func (s *sqlStore) deleteExpiredSessionsSQL() string {
	switch s.dialect {
	case sqlDialectPostgres:
		return `DELETE FROM sessions WHERE expires_at <= to_timestamp($1)`
	default:
		return `DELETE FROM sessions WHERE expires_at <= ?`
	}
}

func scanUser(row scanner) (User, error) {
	var user User
	var createdAtUnix int64
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &createdAtUnix); err != nil {
		return User{}, err
	}

	user.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	return user, nil
}

func scanSession(row scanner) (Session, error) {
	var session Session
	var expiresAtUnix int64
	var createdAtUnix int64
	if err := row.Scan(
		&session.TokenHash,
		&session.UserID,
		&expiresAtUnix,
		&createdAtUnix,
	); err != nil {
		return Session{}, err
	}

	session.ExpiresAt = time.Unix(expiresAtUnix, 0).UTC()
	session.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	return session, nil
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "constraint failed")
}

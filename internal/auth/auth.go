package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/firefoxx04/toyotaview/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const CookieName = "toyotaview_session"

const (
	_maxPasswordBytes  = 72
	_maxUsernameBytes  = 100
	_sessionTokenBytes = 32
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidPassword    = errors.New("password is required and must be at most 72 bytes")
	ErrInvalidSession     = errors.New("invalid session")
	ErrInvalidUsername    = errors.New("username is required and must be at most 100 bytes")
	ErrUsernameTaken      = errors.New("username is unavailable")
)

type Store interface {
	storage.UserStore
	storage.SessionStore
}

type Service struct {
	store      Store
	sessionTTL time.Duration
	now        func() time.Time
}

type SessionResult struct {
	Token   string
	User    storage.User
	Session storage.Session
}

func NewService(store Store, sessionTTL time.Duration) (*Service, error) {
	if store == nil {
		return nil, errors.New("auth store is required")
	}

	if sessionTTL <= 0 {
		return nil, errors.New("auth session ttl must be greater than zero")
	}

	return &Service{
		store:      store,
		sessionTTL: sessionTTL,
		now:        time.Now,
	}, nil
}

func (s *Service) Signup(
	ctx context.Context,
	username string,
	password string,
) (SessionResult, error) {
	normalized := NormalizeUsername(username)
	if err := validateUsername(normalized); err != nil {
		return SessionResult{}, err
	}

	if err := validatePassword(password); err != nil {
		return SessionResult{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return SessionResult{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.store.CreateUser(ctx, storage.CreateUserParams{
		Username:     normalized,
		PasswordHash: string(passwordHash),
	})
	if err != nil {
		if errors.Is(err, storage.ErrDuplicateUser) {
			return SessionResult{}, ErrUsernameTaken
		}

		return SessionResult{}, fmt.Errorf("create user: %w", err)
	}

	return s.createSession(ctx, user)
}

func (s *Service) Login(
	ctx context.Context,
	username string,
	password string,
) (SessionResult, error) {
	normalized := NormalizeUsername(username)
	if err := validateUsername(normalized); err != nil {
		return SessionResult{}, ErrInvalidCredentials
	}

	if err := validatePassword(password); err != nil {
		return SessionResult{}, ErrInvalidCredentials
	}

	user, err := s.store.FindUserByUsername(ctx, normalized)
	if err != nil {
		if errors.Is(err, storage.ErrUserNotFound) {
			return SessionResult{}, ErrInvalidCredentials
		}

		return SessionResult{}, fmt.Errorf("find user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return SessionResult{}, ErrInvalidCredentials
	}

	return s.createSession(ctx, user)
}

func (s *Service) AuthenticateToken(ctx context.Context, token string) (storage.Session, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return storage.Session{}, ErrInvalidSession
	}

	session, err := s.store.FindSessionByTokenHash(ctx, TokenHash(token))
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			return storage.Session{}, ErrInvalidSession
		}

		return storage.Session{}, fmt.Errorf("find session: %w", err)
	}

	return session, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	if err := s.store.DeleteSession(ctx, TokenHash(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func TokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (s *Service) createSession(ctx context.Context, user storage.User) (SessionResult, error) {
	token, err := generateSessionToken()
	if err != nil {
		return SessionResult{}, err
	}

	if err := s.store.DeleteExpiredSessions(ctx); err != nil {
		return SessionResult{}, fmt.Errorf("delete expired sessions: %w", err)
	}

	session, err := s.store.CreateSession(ctx, storage.CreateSessionParams{
		TokenHash: TokenHash(token),
		UserID:    user.ID,
		ExpiresAt: s.now().UTC().Add(s.sessionTTL),
	})
	if err != nil {
		return SessionResult{}, fmt.Errorf("create session: %w", err)
	}

	return SessionResult{
		Token:   token,
		User:    user,
		Session: session,
	}, nil
}

func generateSessionToken() (string, error) {
	token := make([]byte, _sessionTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(token), nil
}

func validateUsername(username string) error {
	if username == "" || len(username) > _maxUsernameBytes {
		return ErrInvalidUsername
	}

	return nil
}

func validatePassword(password string) error {
	if password == "" || len(password) > _maxPasswordBytes {
		return ErrInvalidPassword
	}

	return nil
}

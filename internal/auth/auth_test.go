package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

func TestNormalizeUsername(t *testing.T) {
	t.Parallel()

	got := NormalizeUsername("  DRIVER  ")
	if got != "driver" {
		t.Fatalf("NormalizeUsername() = %q, want driver", got)
	}
}

func TestTokenHash(t *testing.T) {
	t.Parallel()

	first := TokenHash("token")
	second := TokenHash("token")
	if first != second {
		t.Fatal("TokenHash() is not deterministic")
	}

	if first == "token" {
		t.Fatal("TokenHash() returned the raw token")
	}
}

func TestSignupLoginAuthenticateAndLogout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, store := newAuthTestService(t)

	signup, err := service.Signup(ctx, "  Driver  ", "correct-password")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	if signup.Token == "" {
		t.Fatal("Signup() token is empty")
	}

	if signup.User.Username != "driver" {
		t.Fatalf("Signup() username = %q, want driver", signup.User.Username)
	}

	user, err := store.FindUserByUsername(ctx, "driver")
	if err != nil {
		t.Fatalf("FindUserByUsername() error = %v", err)
	}

	if user.PasswordHash == "correct-password" {
		t.Fatal("password was stored in plaintext")
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte("correct-password"),
	); err != nil {
		t.Fatalf("stored password hash mismatch: %v", err)
	}

	if _, err := service.AuthenticateToken(ctx, signup.Token); err != nil {
		t.Fatalf("AuthenticateToken() error = %v", err)
	}

	_, err = service.Login(ctx, "driver", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}

	login, err := service.Login(ctx, "DRIVER", "correct-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if login.Token == signup.Token {
		t.Fatal("Login() reused an existing session token")
	}

	if err := service.Logout(ctx, login.Token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}

	_, err = service.AuthenticateToken(ctx, login.Token)
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("AuthenticateToken() error = %v, want ErrInvalidSession", err)
	}
}

func TestSignupDuplicateUsername(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newAuthTestService(t)

	_, err := service.Signup(ctx, "driver", "correct-password")
	if err != nil {
		t.Fatalf("Signup() error = %v", err)
	}

	_, err = service.Signup(ctx, " DRIVER ", "correct-password")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("Signup() error = %v, want ErrUsernameTaken", err)
	}
}

func TestSignupValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newAuthTestService(t)

	_, err := service.Signup(ctx, " ", "correct-password")
	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("Signup() error = %v, want ErrInvalidUsername", err)
	}

	_, err = service.Signup(ctx, "driver", "")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Signup() error = %v, want ErrInvalidPassword", err)
	}
}

func TestAuthenticateRejectsExpiredSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, store := newAuthTestService(t)
	user, err := store.CreateUser(ctx, storage.CreateUserParams{
		Username:     "driver",
		PasswordHash: "hash",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	_, err = store.CreateSession(ctx, storage.CreateSessionParams{
		TokenHash: TokenHash("expired-token"),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, err = service.AuthenticateToken(ctx, "expired-token")
	if !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("AuthenticateToken() error = %v, want ErrInvalidSession", err)
	}
}

func newAuthTestService(t *testing.T) (*Service, storage.Store) {
	t.Helper()

	store, err := storage.NewSQLite(config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "auth.sqlite3"),
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

	service, err := NewService(store, time.Hour)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	return service, store
}

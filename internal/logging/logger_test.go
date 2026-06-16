package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/firefoxx04/toyotaview/internal/config"
	"go.uber.org/zap"
)

func TestNewWritesJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")

	managed, err := New(config.LoggingConfig{
		File:       logPath,
		Level:      "info",
		MaxSizeMB:  10,
		MaxBackups: 1,
		MaxAgeDays: 1,
		Compress:   false,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	managed.Logger().Info("hello", zap.String("component", "test"))
	if err := managed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "\"msg\":\"hello\"") {
		t.Fatalf("log content = %q, want JSON message", text)
	}

	if !strings.Contains(text, "\"component\":\"test\"") {
		t.Fatalf("log content = %q, want structured field", text)
	}
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	if _, err := parseLevel("nope"); err == nil {
		t.Fatal("parseLevel() error = nil, want error")
	}
}

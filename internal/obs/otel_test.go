package obs

import (
	"context"
	"testing"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"go.uber.org/zap"
)

func TestNewDisabled(t *testing.T) {
	t.Parallel()

	observer, err := New(context.Background(), config.OTELConfig{
		Enabled:        false,
		ServiceName:    "test",
		ServiceVersion: "dev",
		Timeout:        time.Second,
		SampleRatio:    1,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if observer == nil {
		t.Fatal("observer = nil, want value")
	}
}

func TestTLSConfigValidation(t *testing.T) {
	t.Parallel()

	_, err := tlsConfigFromConfig(config.OTELConfig{
		Insecure:       false,
		ClientCertFile: "cert.pem",
	})
	if err == nil {
		t.Fatal("tlsConfigFromConfig() error = nil, want error")
	}
}

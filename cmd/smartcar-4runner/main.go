package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/firefoxx04/toyotaview/internal/app"
	"github.com/firefoxx04/toyotaview/internal/auth"
	"github.com/firefoxx04/toyotaview/internal/config"
	"github.com/firefoxx04/toyotaview/internal/devsmartcar"
	"github.com/firefoxx04/toyotaview/internal/logging"
	"github.com/firefoxx04/toyotaview/internal/obs"
	"github.com/firefoxx04/toyotaview/internal/smartcar"
	"github.com/firefoxx04/toyotaview/internal/storage"
	"github.com/firefoxx04/toyotaview/internal/store"
	"github.com/firefoxx04/toyotaview/internal/web"
	"go.uber.org/zap"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:], envMap(os.Environ()))
	if err != nil {
		var helpErr config.HelpError
		if errors.As(err, &helpErr) {
			fmt.Print(helpErr.Message)
			return nil
		}

		return err
	}

	if cfg.PrintConfig {
		payload, err := cfg.RedactedJSON()
		if err != nil {
			return err
		}

		fmt.Println(string(payload))
		return nil
	}

	managedLogger, err := logging.New(cfg.Logging)
	if err != nil {
		return err
	}
	defer func() {
		_ = managedLogger.Close()
	}()

	logger := managedLogger.Logger()
	logger.Info("startup", zap.Any("config", cfg.Redacted()))
	if !cfg.IsLoopback() {
		logger.Warn("binding to non-loopback address", zap.String("addr", cfg.Addr))
	}

	observer, err := obs.New(context.Background(), cfg.OTEL, logger)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = observer.Shutdown(shutdownCtx)
	}()

	durableStore, err := storage.Open(cfg.Storage)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := durableStore.Close(); closeErr != nil {
			logger.Warn("close storage", zap.Error(closeErr))
		}
	}()

	storageCtx, cancelStorage := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelStorage()
	if err := durableStore.Ping(storageCtx); err != nil {
		return err
	}

	if shouldMigrateStorage(cfg) {
		if err := durableStore.Migrate(storageCtx); err != nil {
			return err
		}
	}

	authService, err := auth.NewService(durableStore, cfg.Auth.SessionTTL)
	if err != nil {
		return err
	}

	smartcarClient, err := newSmartcarAPI(cfg, logger, observer)
	if err != nil {
		return err
	}

	memoryStore := store.NewMemoryStore()
	service := app.NewService(cfg.Smartcar, smartcarClient, memoryStore, logger, observer)
	handler, err := web.NewHandler(
		service,
		memoryStore,
		authService,
		!cfg.IsLoopback(),
		logger,
		observer,
		web.VersionInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
	)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ErrorLog:          managedLogger.Stdlib(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server listening", zap.String("addr", cfg.Addr))
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErrors <- serveErr
		}
		close(serverErrors)
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		logger.Info("shutdown requested")
	case serveErr, ok := <-serverErrors:
		if ok && serveErr != nil {
			return serveErr
		}
		if !ok {
			return nil
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	logger.Info("graceful shutdown starting")
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	logger.Info("graceful shutdown complete")
	return nil
}

func shouldMigrateStorage(cfg config.Config) bool {
	if cfg.Storage.Driver == config.StorageDriverSQLite {
		return true
	}

	return cfg.Storage.Driver == config.StorageDriverPostgres &&
		cfg.Storage.Postgres.MigrateOnStart
}

func newSmartcarAPI(
	cfg config.Config,
	logger *zap.Logger,
	observer *obs.Observer,
) (smartcar.API, error) {
	if cfg.Dev.Enabled {
		logger.Info("starting with mocked Smartcar backend",
			zap.String("scenario", cfg.Dev.Scenario),
		)

		return devsmartcar.New(devsmartcar.Config{
			Scenario:    cfg.Dev.Scenario,
			SignalCodes: cfg.Smartcar.SignalCodes,
			UnitSystem:  cfg.Smartcar.UnitSystem,
		})
	}

	httpClient := smartcar.NewHTTPClient(
		cfg.Smartcar.Timeout,
		&tls.Config{MinVersion: tls.VersionTLS12},
	)

	return smartcar.NewClient(httpClient, cfg.Smartcar, version, logger, observer)
}

func envMap(values []string) map[string]string {
	env := make(map[string]string, len(values))
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}

		env[key] = value
	}

	return env
}

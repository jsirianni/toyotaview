package logging

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/firefoxx04/toyotaview/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type ManagedLogger struct {
	logger  *zap.Logger
	closers []io.Closer
}

func New(cfg config.LoggingConfig) (*ManagedLogger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	fileWriter := &lumberjack.Logger{
		Filename:   cfg.File,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}

	syncers := []zapcore.WriteSyncer{zapcore.AddSync(fileWriter)}
	closers := []io.Closer{fileWriter}
	if cfg.AddStdout {
		syncers = append(syncers, zapcore.AddSync(os.Stdout))
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "ts"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(syncers...),
		level,
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	return &ManagedLogger{
		logger:  logger,
		closers: closers,
	}, nil
}

func (m *ManagedLogger) Logger() *zap.Logger {
	if m == nil {
		return zap.NewNop()
	}

	return m.logger
}

func (m *ManagedLogger) Close() error {
	if m == nil {
		return nil
	}

	var errs []error
	if err := m.logger.Sync(); err != nil && !isIgnorableSyncError(err) {
		errs = append(errs, err)
	}

	for _, closer := range m.closers {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (m *ManagedLogger) Stdlib() *log.Logger {
	return zap.NewStdLog(m.Logger())
}

func parseLevel(value string) (zapcore.Level, error) {
	var level zapcore.Level
	if err := level.Set(value); err != nil {
		return 0, fmt.Errorf("parse log level: %w", err)
	}

	return level, nil
}

func isIgnorableSyncError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())

	return strings.Contains(message, "invalid argument") ||
		strings.Contains(message, "incorrect function")
}

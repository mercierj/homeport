// Package logger provides structured logging using slog.
package logger

import (
	"context"
	"log/slog"
	"os"
)

// Logger is the global logger instance.
var Logger *slog.Logger

// Config holds logger configuration.
type Config struct {
	Level   slog.Level
	JSON    bool
	Verbose bool
}

// Init initializes the global logger with the given configuration.
func Init(cfg Config) {
	var handler slog.Handler

	level := cfg.Level
	if cfg.Verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.Verbose,
	}

	if cfg.JSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	Logger = slog.New(handler)
	slog.SetDefault(Logger)
}

// Default returns a basic default logger if Init hasn't been called.
func Default() *slog.Logger {
	if Logger == nil {
		Init(Config{Level: slog.LevelInfo})
	}
	return Logger
}

// WithRequestID returns a logger with the request ID attached.
func WithRequestID(ctx context.Context, requestID string) *slog.Logger {
	return Default().With(slog.String("request_id", requestID))
}

// Info logs at INFO level.
func Info(msg string, args ...any) {
	Default().Info(msg, args...)
}

// Debug logs at DEBUG level.
func Debug(msg string, args ...any) {
	Default().Debug(msg, args...)
}

// Warn logs at WARN level.
func Warn(msg string, args ...any) {
	Default().Warn(msg, args...)
}

// Error logs at ERROR level.
func Error(msg string, args ...any) {
	Default().Error(msg, args...)
}

// With returns a logger with additional attributes.
func With(args ...any) *slog.Logger {
	return Default().With(args...)
}

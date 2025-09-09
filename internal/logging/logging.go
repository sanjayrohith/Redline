// Package logging configures the gateway's structured slog logger,
// including redaction of secret-shaped fields.
package logging

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var secretKeyPattern = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key|authorization|credential)`)

// New builds a JSON slog.Logger at the given level, masking the value of
// any attribute whose key looks like it carries a secret.
func New(level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       parseLevel(level),
		ReplaceAttr: redact,
	})
	return slog.New(handler)
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if secretKeyPattern.MatchString(a.Key) {
		a.Value = slog.StringValue("[REDACTED]")
	}
	return a
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type contextKey struct{}

// WithContext returns a context carrying logger, retrievable via FromContext.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// FromContext returns the logger stashed by WithContext, or slog.Default()
// if the context carries none.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

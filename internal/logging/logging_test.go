package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: redact})
	logger := slog.New(handler)

	logger.Info("issued key", "api_key", "supersecret", "user_id", "42")

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal log line: %v", err)
	}

	if out["api_key"] != "[REDACTED]" {
		t.Errorf("api_key = %v, want [REDACTED]", out["api_key"])
	}
	if out["user_id"] != "42" {
		t.Errorf("user_id = %v, want 42 (should not be redacted)", out["user_id"])
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"bogus": slog.LevelInfo,
		"":      slog.LevelInfo,
	}
	for input, want := range tests {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	ctx := WithContext(context.Background(), logger)
	got := FromContext(ctx)
	got.Info("hello")

	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("expected logger from context to be used, got %q", buf.String())
	}
}

func TestFromContextDefaultsWhenAbsent(t *testing.T) {
	if FromContext(context.Background()) == nil {
		t.Fatal("FromContext() = nil, want slog.Default()")
	}
}

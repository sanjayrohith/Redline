package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("defaults with no file", func(t *testing.T) {
		t.Setenv("REDLINE_DATABASE_URL", "postgres://test/db")
		t.Setenv("REDLINE_JWT_SIGNING_KEY", "test-signing-key")
		t.Setenv("REDLINE_REDIS_ADDR", "localhost:6379")

		cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ListenAddr != ":8080" {
			t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
		}
	})

	t.Run("yaml file overrides defaults", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("listen_addr: :9090\ndatabase_url: postgres://test/db\njwt_signing_key: test-signing-key\nredis_addr: localhost:6379\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ListenAddr != ":9090" {
			t.Errorf("ListenAddr = %q, want :9090", cfg.ListenAddr)
		}
	})

	t.Run("env overrides yaml file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("listen_addr: :9090\ndatabase_url: postgres://test/db\njwt_signing_key: test-signing-key\nredis_addr: localhost:6379\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REDLINE_LISTEN_ADDR", ":7070")

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.ListenAddr != ":7070" {
			t.Errorf("ListenAddr = %q, want :7070", cfg.ListenAddr)
		}
	})

	t.Run("fails fast on missing required key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("listen_addr: \"\"\ndatabase_url: postgres://test/db\njwt_signing_key: test-signing-key\nredis_addr: localhost:6379\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("Load() error = nil, want error for empty listen_addr")
		}
	})

	t.Run("fails fast on missing database url", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("environment: test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("Load() error = nil, want error for empty database_url")
		}
	})
}

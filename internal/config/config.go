// Package config implements the gateway's layered configuration loader:
// defaults, then an optional YAML file, then environment overrides.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the gateway's fully resolved configuration.
type Config struct {
	ListenAddr  string `yaml:"listen_addr"`
	Environment string `yaml:"environment"`
	LogLevel    string `yaml:"log_level"`

	DatabaseURL      string        `yaml:"database_url"`
	DBMaxConns       int32         `yaml:"db_max_conns"`
	DBConnectTimeout time.Duration `yaml:"db_connect_timeout"`
}

func defaults() Config {
	return Config{
		ListenAddr:  ":8080",
		Environment: "development",
		LogLevel:    "info",

		DBMaxConns:       10,
		DBConnectTimeout: 5 * time.Second,
	}
}

// Load resolves configuration by starting from defaults, layering in the
// YAML file at path if it exists, then applying environment overrides.
// It fails fast if any required key ends up empty.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		data, err := os.ReadFile(path) // #nosec G304 -- path is operator-supplied at startup, never request input
		switch {
		case err == nil:
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("config: parse %s: %w", path, err)
			}
		case os.IsNotExist(err):
			// No file at path is not an error; defaults and env stand alone.
		default:
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
	}

	applyEnvOverrides(&cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("REDLINE_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("REDLINE_ENVIRONMENT"); v != "" {
		cfg.Environment = v
	}
	if v := os.Getenv("REDLINE_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("REDLINE_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("REDLINE_DB_MAX_CONNS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			cfg.DBMaxConns = int32(n)
		}
	}
}

func (c Config) validate() error {
	var missing []string

	if c.ListenAddr == "" {
		missing = append(missing, "listen_addr")
	}
	if c.Environment == "" {
		missing = append(missing, "environment")
	}
	if c.LogLevel == "" {
		missing = append(missing, "log_level")
	}
	if c.DatabaseURL == "" {
		missing = append(missing, "database_url")
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}

	return nil
}

// Package config implements the gateway's layered configuration loader:
// defaults, then an optional YAML file, then environment overrides.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the gateway's fully resolved configuration.
type Config struct {
	ListenAddr  string `yaml:"listen_addr"`
	Environment string `yaml:"environment"`
	LogLevel    string `yaml:"log_level"`
}

func defaults() Config {
	return Config{
		ListenAddr:  ":8080",
		Environment: "development",
		LogLevel:    "info",
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

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}

	return nil
}

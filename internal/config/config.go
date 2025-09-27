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

	JWTSigningKey   string        `yaml:"jwt_signing_key"`
	JWTIssuer       string        `yaml:"jwt_issuer"`
	JWTAudience     string        `yaml:"jwt_audience"`
	AccessTokenTTL  time.Duration `yaml:"access_token_ttl"`
	RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl"`

	RedisAddr        string        `yaml:"redis_addr"`
	RedisPassword    string        `yaml:"redis_password"`
	RedisDB          int           `yaml:"redis_db"`
	RedisPoolSize    int           `yaml:"redis_pool_size"`
	RedisMaxRetries  int           `yaml:"redis_max_retries"`
	RedisDialTimeout time.Duration `yaml:"redis_dial_timeout"`

	RequestTimeout     time.Duration `yaml:"request_timeout"`
	CORSAllowedOrigins []string      `yaml:"cors_allowed_origins"`

	ChatRateLimit         int           `yaml:"chat_rate_limit"`
	ChatRateLimitWindow   time.Duration `yaml:"chat_rate_limit_window"`
	MockBackendTokenDelay time.Duration `yaml:"mock_backend_token_delay"`
}

func defaults() Config {
	return Config{
		ListenAddr:  ":8080",
		Environment: "development",
		LogLevel:    "info",

		DBMaxConns:       10,
		DBConnectTimeout: 5 * time.Second,

		JWTIssuer:       "redline",
		JWTAudience:     "redline-api",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,

		RedisPoolSize:    10,
		RedisMaxRetries:  3,
		RedisDialTimeout: 5 * time.Second,

		RequestTimeout: 30 * time.Second,

		ChatRateLimit:       60,
		ChatRateLimitWindow: time.Minute,
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
	if v := os.Getenv("REDLINE_JWT_SIGNING_KEY"); v != "" {
		cfg.JWTSigningKey = v
	}
	if v := os.Getenv("REDLINE_REDIS_ADDR"); v != "" {
		cfg.RedisAddr = v
	}
	if v := os.Getenv("REDLINE_REDIS_PASSWORD"); v != "" {
		cfg.RedisPassword = v
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
	if c.JWTSigningKey == "" {
		missing = append(missing, "jwt_signing_key")
	}
	if c.RedisAddr == "" {
		missing = append(missing, "redis_addr")
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}

	return nil
}

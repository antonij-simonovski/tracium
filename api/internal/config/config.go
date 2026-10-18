package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// AuthModeJWT, the only authentication mode, verifies the tokens issued by
// /v1/auth/login against the accounts stored in Postgres.
const AuthModeJWT = "jwt"

// insecureJWTSecret is the value this repository used to default to. Tracium is
// OSS, so the string is public and tokens signed with it are forgeable by
// anyone — Validate rejects it.
const insecureJWTSecret = "dev-insecure-secret-change-me"

// secretHint is appended to jwt_secret errors so the operator can fix the
// deploy without leaving the log line.
const secretHint = "generate one with: openssl rand -hex 32"

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Addr                string `yaml:"addr"`
	ReadTimeoutSeconds  int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds int    `yaml:"write_timeout_seconds"`
}

// StorageConfig holds database connection strings.
type StorageConfig struct {
	ClickHouseDSN string `yaml:"clickhouse_dsn"`
	PostgresDSN   string `yaml:"postgres_dsn"`
	QueryTimeout  int    `yaml:"query_timeout_seconds"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Mode      string `yaml:"mode"` // must be AuthModeJWT
	JWTSecret string `yaml:"jwt_secret"`
	// RateLimitPerMinute caps register/login attempts per client IP per minute.
	// Defaults to 10; set a negative value to disable throttling entirely.
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
	// VerifyRateLimitPerMinute caps ingest-key verification attempts per client
	// IP per minute, on a bucket separate from login. Defaults to 120; a negative
	// value disables throttling of the verify endpoint.
	VerifyRateLimitPerMinute int `yaml:"verify_rate_limit_per_minute"`
	// TrustedProxies lists IPs/CIDRs or dns:service-name entries for proxies allowed to set
	// X-Forwarded-For for the rate limiter. Empty (the default) means the header
	// is ignored and the socket peer is always used, so a direct client cannot
	// spoof its source IP. Set this to your ingress/proxy network only when that
	// proxy overwrites the header from untrusted input.
	TrustedProxies []string `yaml:"trusted_proxies"`
}

// Config is the top-level application configuration.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Auth    AuthConfig    `yaml:"auth"`
}

// Default fills every unset field that has a default.
func (c *Config) Default() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8090"
	}
	if c.Server.ReadTimeoutSeconds == 0 {
		c.Server.ReadTimeoutSeconds = 30
	}
	if c.Server.WriteTimeoutSeconds == 0 {
		c.Server.WriteTimeoutSeconds = 30
	}
	if c.Storage.QueryTimeout == 0 {
		c.Storage.QueryTimeout = 10
	}
	if c.Auth.Mode == "" {
		c.Auth.Mode = AuthModeJWT
	}
	if c.Auth.RateLimitPerMinute == 0 {
		c.Auth.RateLimitPerMinute = 10
	}
	if c.Auth.VerifyRateLimitPerMinute == 0 {
		c.Auth.VerifyRateLimitPerMinute = 120
	}
}

// Validate checks the HTTP server settings.
func (c *ServerConfig) Validate() error {
	var errs []error
	if c.Addr == "" {
		errs = append(errs, errors.New("config: server.addr is required (set LISTEN_ADDR)"))
	}
	if c.ReadTimeoutSeconds <= 0 {
		errs = append(errs, errors.New("config: server.read_timeout_seconds must be positive"))
	}
	if c.WriteTimeoutSeconds <= 0 {
		errs = append(errs, errors.New("config: server.write_timeout_seconds must be positive"))
	}
	return errors.Join(errs...)
}

// Validate checks the storage settings. Both DSNs are required: the API cannot
// serve traces without ClickHouse, and cannot authenticate anyone without the
// accounts in Postgres. Starting without either would fail open.
func (c *StorageConfig) Validate() error {
	var errs []error
	if c.ClickHouseDSN == "" {
		errs = append(errs, errors.New("config: storage.clickhouse_dsn is required (set CLICKHOUSE_DSN)"))
	}
	if c.PostgresDSN == "" {
		errs = append(errs, errors.New("config: storage.postgres_dsn is required (set POSTGRES_DSN)"))
	}
	if c.QueryTimeout <= 0 {
		errs = append(errs, errors.New("config: storage.query_timeout_seconds must be positive"))
	}
	return errors.Join(errs...)
}

// Validate checks the auth settings.
func (c *AuthConfig) Validate() error {
	var errs []error
	if c.Mode != AuthModeJWT {
		errs = append(errs, fmt.Errorf("config: auth.mode must be %q, got %q (set AUTH_MODE)", AuthModeJWT, c.Mode))
	}
	switch c.JWTSecret {
	case "":
		errs = append(errs, fmt.Errorf("config: auth.jwt_secret is required (set JWT_SECRET) — %s", secretHint))
	case insecureJWTSecret:
		errs = append(errs, fmt.Errorf("config: auth.jwt_secret is the published example value, so anyone can forge admin tokens — %s", secretHint))
	}
	return errors.Join(errs...)
}

// Validate checks that the configuration is valid and returns an error if not.
// Every invalid field is reported at once so the operator sees all of them.
func (c *Config) Validate() error {
	return errors.Join(
		c.Server.Validate(),
		c.Storage.Validate(),
		c.Auth.Validate(),
	)
}

// Load parses the YAML config file at path (none when empty) and applies
// defaults to the fields it leaves unset.
func Load(path string) (*Config, error) {
	c := &Config{}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: read file %q: %w", path, err)
		}
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("config: parse yaml %q: %w", path, err)
		}
	}
	c.Default()
	return c, nil
}

// ApplyEnv overrides config fields from well-known environment variables.
func (c *Config) ApplyEnv() error {
	if v := os.Getenv("CLICKHOUSE_DSN"); v != "" {
		c.Storage.ClickHouseDSN = v
	}
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		c.Storage.PostgresDSN = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		c.Auth.JWTSecret = v
	}
	if v := os.Getenv("AUTH_MODE"); v != "" {
		c.Auth.Mode = v
	}
	if err := envInt("AUTH_RATE_LIMIT_PER_MINUTE", &c.Auth.RateLimitPerMinute); err != nil {
		return err
	}
	if err := envInt("AUTH_VERIFY_RATE_LIMIT_PER_MINUTE", &c.Auth.VerifyRateLimitPerMinute); err != nil {
		return err
	}
	if v := os.Getenv("AUTH_TRUSTED_PROXIES"); v != "" {
		var proxies []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				proxies = append(proxies, p)
			}
		}
		c.Auth.TrustedProxies = proxies
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		c.Server.Addr = v
	}
	return nil
}

func envInt(name string, dst *int) error {
	v := os.Getenv(name)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("config: %s must be an integer, got %q", name, v)
	}
	*dst = n
	return nil
}

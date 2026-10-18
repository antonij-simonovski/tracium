package config

import (
	"strings"
	"testing"
)

// validConfig returns a config that passes Validate, for tests to break one
// field at a time.
func validConfig() *Config {
	c := &Config{}
	c.Default()
	c.Storage.ClickHouseDSN = "clickhouse://localhost:9000/tracium"
	c.Storage.PostgresDSN = "postgres://localhost:5432/tracium"
	c.Auth.JWTSecret = "6f1c1d9b0a4e4f9a8c3b2d1e0f9a8b7c"
	return c
}

func TestValidate_Valid(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestDefault_DoesNotSupplyJWTSecret(t *testing.T) {
	c := &Config{}
	c.Default()
	if c.Auth.JWTSecret != "" {
		t.Fatalf("Default() must not invent a signing secret, got %q", c.Auth.JWTSecret)
	}
	if c.Auth.Mode != AuthModeJWT {
		t.Fatalf("Default() auth mode = %q, want %q", c.Auth.Mode, AuthModeJWT)
	}
}

// Required fields must fail the deploy rather than silently fall back — a
// missing Postgres DSN used to disable authentication entirely.
func TestValidate_RequiredFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing clickhouse dsn", func(c *Config) { c.Storage.ClickHouseDSN = "" }, "clickhouse_dsn"},
		{"missing postgres dsn", func(c *Config) { c.Storage.PostgresDSN = "" }, "postgres_dsn"},
		{"missing jwt secret", func(c *Config) { c.Auth.JWTSecret = "" }, "jwt_secret"},
		{"published jwt secret", func(c *Config) { c.Auth.JWTSecret = insecureJWTSecret }, "jwt_secret"},
		{"unknown auth mode", func(c *Config) { c.Auth.Mode = "yes-please" }, "auth.mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// Rule 2: the operator sees every problem at once, not just the first.
func TestValidate_ReportsAllProblems(t *testing.T) {
	c := &Config{}
	c.Default()

	err := c.Validate()
	if err == nil {
		t.Fatal("expected errors for an unconfigured Config")
	}
	for _, want := range []string{"clickhouse_dsn", "postgres_dsn", "jwt_secret"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%v", want, err)
		}
	}
}

func TestApplyEnv_AuthModeNoneIsRejected(t *testing.T) {
	t.Setenv("AUTH_MODE", "none")

	c := validConfig()
	if err := c.ApplyEnv(); err != nil {
		t.Fatal(err)
	}

	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "auth.mode") {
		t.Fatalf("auth.mode=none must fail validation, got: %v", err)
	}
}

func TestApplyEnv_RejectsNonIntegerRateLimit(t *testing.T) {
	for _, name := range []string{"AUTH_RATE_LIMIT_PER_MINUTE", "AUTH_VERIFY_RATE_LIMIT_PER_MINUTE"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "ten")
			err := (&Config{}).ApplyEnv()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected an error naming %s, got %v", name, err)
			}
		})
	}
}

package traciumprocessor

import "fmt"

// Config is the traciumprocessor configuration block, set under
// processors.tracium in the collector config.
type Config struct {
	// AllowedModels optionally restricts which models are kept. Empty = allow all.
	AllowedModels []string `mapstructure:"allowed_models"`

	// Pricing controls how per-span cost is computed.
	Pricing PricingConfig `mapstructure:"pricing"`

	// User controls user resolution. When unset, the user carried in the
	// span/resource attributes (if any) is used as-is.
	User UserConfig `mapstructure:"user"`

	// DeadLetter controls where spans the enrichment chain rejects are kept.
	// Drops are always counted and logged; a path additionally persists the
	// whole span so it can be recovered.
	DeadLetter DeadLetterConfig `mapstructure:"dead_letter"`

	// CaptureContent persists raw prompt/completion content (the OTel
	// gen_ai.input.messages / gen_ai.output.messages and OpenLLMetry indexed
	// gen_ai.prompt.* / gen_ai.completion.* attributes) to storage. Off by
	// default: this text may contain sensitive data, so operators must opt in.
	// When false, the processor strips those attributes (see genai.IsContentKey)
	// before the span reaches the exporter.
	CaptureContent bool `mapstructure:"capture_content"`
}

// PricingConfig selects a pricing source. Only "static" is supported.
type PricingConfig struct {
	// Source is the pricing strategy: "static" (the default).
	Source string `mapstructure:"source"`
	// StaticFilePath is a JSON price table for Source=="static". When empty,
	// built-in DefaultPrices are used.
	StaticFilePath string `mapstructure:"static_file_path"`
}

// UserConfig selects a user-resolution source. Only "passthrough" (use the
// attribute value as the user ID) is supported.
type UserConfig struct {
	// Source is the user strategy: "passthrough" (the default).
	Source string `mapstructure:"source"`
}

// DeadLetterConfig selects the dead-letter sink for rejected spans. With no
// path they are logged (visible and countable, but not recoverable); with a
// path they are also appended to that file as NDJSON.
type DeadLetterConfig struct {
	// Path is a file the collector appends rejected spans to as NDJSON. Empty
	// (the default) uses the log store. A path that cannot be opened fails at
	// startup, not on the first dropped span.
	Path string `mapstructure:"path"`

	// ClickHouseDSN, when set, counts rejections per workspace for setup checks.
	ClickHouseDSN string `mapstructure:"clickhouse_dsn"`
}

// Validate implements component.ConfigValidator.
func (c *Config) Validate() error {
	switch c.Pricing.Source {
	case "", "static":
	default:
		return fmt.Errorf("pricing.source %q is not supported", c.Pricing.Source)
	}
	switch c.User.Source {
	case "", "passthrough":
	default:
		return fmt.Errorf("user.source %q is not supported", c.User.Source)
	}
	return nil
}

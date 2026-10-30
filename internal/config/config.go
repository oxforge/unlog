// Package config provides TOML-based configuration loading with environment
// variable overlay for the unlog CLI tool.
//
// Priority order (highest to lowest):
//
//	CLI flags (handled by cobra/cmd layer)
//	Environment variables (UNLOG_*)
//	~/.unlog/config.toml
//	Defaults
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all unlog configuration. Flat structure — all fields can be
// set via TOML file or environment variable.
type Config struct {
	Level     string `toml:"level"`      // min log level: trace, debug, info, warn, error, fatal
	Format    string `toml:"format"`     // output format: text, json
	NoColor   bool   `toml:"no_color"`   // disable colored output
	NoiseFile string `toml:"noise_file"` // custom noise patterns path
	Since     string `toml:"since"`      // time filter start (ISO 8601 or relative)
	Until     string `toml:"until"`      // time filter end (ISO 8601 or relative)
	TZ        string `toml:"tz"`         // zone of timestamps that carry none (default UTC)
	Verbose   bool   `toml:"verbose"`    // verbose output

	// Retired names the settings from before v0.4.0, when unlog called LLMs,
	// that are still set to a value: config keys first, then env vars. They
	// have no effect.
	Retired []string `toml:"-"`
}

// retiredKeys are the config keys of the LLM stage removed in v0.4.0.
type retiredKeys struct {
	AIProvider   string `toml:"ai_provider"`
	Model        string `toml:"model"`
	SystemPrompt string `toml:"system_prompt"`
	AITimeout    string `toml:"ai_timeout"`
}

// retiredEnv are the env vars of the LLM stage removed in v0.4.0.
var retiredEnv = []string{"UNLOG_AI_PROVIDER", "UNLOG_MODEL", "UNLOG_SYSTEM_PROMPT", "UNLOG_AI_TIMEOUT"}

// Load reads the TOML file at path (if it exists), then overlays env vars.
// A missing file is not an error — returns zero Config with env overlay.
// Malformed TOML returns an error.
func Load(path string) (Config, error) {
	var cfg Config

	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := toml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("config: parse %q: %w", path, err)
			}
			var old retiredKeys
			if err := toml.Unmarshal(data, &old); err != nil {
				return Config{}, fmt.Errorf("config: parse %q: %w", path, err)
			}
			for _, k := range []struct{ name, value string }{
				{"ai_provider", old.AIProvider}, {"model", old.Model},
				{"system_prompt", old.SystemPrompt}, {"ai_timeout", old.AITimeout},
			} {
				if k.value != "" {
					cfg.Retired = append(cfg.Retired, k.name)
				}
			}
		} else if !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config: read %q: %w", path, err)
		}
	}

	overlayEnv(&cfg)
	return cfg, nil
}

// overlayEnv applies UNLOG_* environment variables over cfg.
func overlayEnv(cfg *Config) {
	if v := os.Getenv("UNLOG_LEVEL"); v != "" {
		cfg.Level = v
	}
	if v := os.Getenv("UNLOG_FORMAT"); v != "" {
		cfg.Format = v
	}
	if v := os.Getenv("UNLOG_NO_COLOR"); v != "" {
		if b, ok := parseBool(v, "UNLOG_NO_COLOR"); ok {
			cfg.NoColor = b
		}
	}
	if v := os.Getenv("UNLOG_NOISE_FILE"); v != "" {
		cfg.NoiseFile = v
	}
	if v := os.Getenv("UNLOG_SINCE"); v != "" {
		cfg.Since = v
	}
	if v := os.Getenv("UNLOG_UNTIL"); v != "" {
		cfg.Until = v
	}
	if v := os.Getenv("UNLOG_TZ"); v != "" {
		cfg.TZ = v
	}
	for _, name := range retiredEnv {
		if os.Getenv(name) != "" {
			cfg.Retired = append(cfg.Retired, name)
		}
	}
	if v := os.Getenv("UNLOG_VERBOSE"); v != "" {
		if b, ok := parseBool(v, "UNLOG_VERBOSE"); ok {
			cfg.Verbose = b
		}
	}
}

// parseBool parses a bool env var value ("1"/"true"/"yes" or "0"/"false"/"no").
func parseBool(s, name string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true, true
	case "0", "false", "no":
		return false, true
	default:
		slog.Warn("config: unrecognised bool value, ignoring", "env", name, "value", s)
		return false, false
	}
}

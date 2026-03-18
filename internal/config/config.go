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
	Level        string `toml:"level"`         // min log level: trace, debug, info, warn, error, fatal
	Format       string `toml:"format"`        // output format: text, json
	NoColor      bool   `toml:"no_color"`      // disable colored output
	NoiseFile    string `toml:"noise_file"`    // custom noise patterns path
	Since        string `toml:"since"`         // time filter start (ISO 8601 or relative)
	Until        string `toml:"until"`         // time filter end (ISO 8601 or relative)
	AIProvider   string `toml:"ai_provider"`   // LLM provider: openai, anthropic, ollama (empty = no AI)
	Model        string `toml:"model"`         // LLM model override
	SystemPrompt string `toml:"system_prompt"` // custom LLM system prompt
	AITimeout    string `toml:"ai_timeout"`    // timeout for LLM API calls (e.g. "2m", "30s")
	Verbose      bool   `toml:"verbose"`       // verbose output
}

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
	if v := os.Getenv("UNLOG_AI_PROVIDER"); v != "" {
		cfg.AIProvider = v
	}
	if v := os.Getenv("UNLOG_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("UNLOG_SYSTEM_PROMPT"); v != "" {
		cfg.SystemPrompt = v
	}
	if v := os.Getenv("UNLOG_AI_TIMEOUT"); v != "" {
		cfg.AITimeout = v
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

package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/filter"
	"github.com/oxforge/unlog/types"
)

var supportedFormats = map[string]bool{
	"text":     true,
	"json":     true,
	"markdown": true,
}

// resolveFormat merges the --format flag with config and validates.
func resolveFormat(cmd *cobra.Command) (string, error) {
	ef := cfg.Format
	if cmd.Flags().Changed("format") {
		ef = formatFlag
	}
	if ef == "" {
		ef = "text"
	}
	if !supportedFormats[ef] {
		return "", fmt.Errorf("cmd: unsupported output format: %q (valid: text, json, markdown)", ef)
	}
	return ef, nil
}

// buildFilterOpts merges CLI flags over config and returns pipeline-ready filter options.
func buildFilterOpts(cmd *cobra.Command, args []string) (filter.FilterOptions, error) {
	effectiveLevel := cfg.Level
	if cmd.Flags().Changed("level") {
		effectiveLevel = levelFlag
	}
	if effectiveLevel == "" {
		effectiveLevel = "warn"
	}

	effectiveSince := cfg.Since
	if cmd.Flags().Changed("since") {
		effectiveSince = sinceFlag
	}

	effectiveUntil := cfg.Until
	if cmd.Flags().Changed("until") {
		effectiveUntil = untilFlag
	}

	effectiveNoise := cfg.NoiseFile
	if cmd.Flags().Changed("noise-file") {
		effectiveNoise = noiseFileFlag
	}

	minLevel := types.ParseLevel(effectiveLevel)
	if minLevel == types.LevelUnknown {
		return filter.FilterOptions{}, fmt.Errorf("cmd: invalid log level: %q", effectiveLevel)
	}

	var since, until time.Time
	if effectiveSince != "" {
		t, err := parseTimeFlag(effectiveSince, time.Now())
		if err != nil {
			return filter.FilterOptions{}, fmt.Errorf("cmd: invalid --since value: %w", err)
		}
		since = t
	}
	if effectiveUntil != "" {
		t, err := parseTimeFlag(effectiveUntil, time.Now())
		if err != nil {
			return filter.FilterOptions{}, fmt.Errorf("cmd: invalid --until value: %w", err)
		}
		until = t
	}

	isStdin := len(args) == 0 || (len(args) == 1 && args[0] == "-")

	opts := filter.DefaultFilterOptions()
	opts.MinLevel = minLevel
	opts.Since = since
	opts.Until = until
	opts.NoiseFile = effectiveNoise
	opts.IsStdin = isStdin

	return opts, nil
}

// resolveProviderName returns the effective AI provider name (empty means no AI).
func resolveProviderName(cmd *cobra.Command) string {
	if cmd.Flags().Changed("ai-provider") {
		return aiProviderFlag
	}
	return cfg.AIProvider
}

// resolveModelName returns the effective model name override.
func resolveModelName(cmd *cobra.Command) string {
	if cmd.Flags().Changed("model") {
		return modelFlag
	}
	return cfg.Model
}

// resolveAITimeout returns the effective AI timeout duration.
func resolveAITimeout(cmd *cobra.Command) (time.Duration, error) {
	s := cfg.AITimeout
	if cmd.Flags().Changed("ai-timeout") {
		s = aiTimeoutFlag
	}
	if s == "" {
		return 0, nil // 0 signals providers to use their default
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("cmd: invalid --ai-timeout value: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("cmd: --ai-timeout must be positive")
	}
	return d, nil
}

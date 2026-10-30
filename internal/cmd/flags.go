package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/internal/filter"
	"github.com/oxforge/unlog/internal/types"
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

// resolveLocation returns the zone for timestamps that carry none (--tz,
// UNLOG_TZ, config tz); UTC by default.
func resolveLocation(cmd *cobra.Command) (*time.Location, error) {
	name, from := cfg.TZ, "tz value in the config file"
	if os.Getenv("UNLOG_TZ") != "" {
		from = "UNLOG_TZ value"
	}
	if cmd.Flags().Changed("tz") {
		name, from = tzFlag, "--tz value"
	}
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("cmd: invalid %s: %w", from, err)
	}
	return loc, nil
}

// buildFilterOpts merges CLI flags over config and returns pipeline-ready
// filter options; zone-less --since/--until values are read in loc.
func buildFilterOpts(cmd *cobra.Command, loc *time.Location) (filter.FilterOptions, error) {
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
		t, err := parseTimeFlag(effectiveSince, time.Now(), loc)
		if err != nil {
			return filter.FilterOptions{}, fmt.Errorf("cmd: invalid --since value: %w", err)
		}
		since = t
	}
	if effectiveUntil != "" {
		t, err := parseTimeFlag(effectiveUntil, time.Now(), loc)
		if err != nil {
			return filter.FilterOptions{}, fmt.Errorf("cmd: invalid --until value: %w", err)
		}
		until = t
	}

	opts := filter.DefaultFilterOptions()
	opts.MinLevel = minLevel
	opts.Since = since
	opts.Until = until
	opts.NoiseFile = effectiveNoise

	return opts, nil
}

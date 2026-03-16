package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/filter"
	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
	"github.com/oxforge/unlog/types"
)

var supportedFormats = map[string]bool{
	"text":     true,
	"json":     true,
	"markdown": true,
}

// resolveFormat merges the --format flag with config and validates.
func resolveFormat(cmd *cobra.Command, flagVal string) (string, error) {
	ef := cfg.Format
	if cmd.Flags().Changed("format") {
		ef = flagVal
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
func buildFilterOpts(cmd *cobra.Command, flagLevel, flagSince, flagUntil, flagNoise string, args []string) (filter.FilterOptions, error) {
	effectiveLevel := cfg.Level
	if cmd.Flags().Changed("level") {
		effectiveLevel = flagLevel
	}
	if effectiveLevel == "" {
		effectiveLevel = "warn"
	}

	effectiveSince := cfg.Since
	if cmd.Flags().Changed("since") {
		effectiveSince = flagSince
	}

	effectiveUntil := cfg.Until
	if cmd.Flags().Changed("until") {
		effectiveUntil = flagUntil
	}

	effectiveNoise := cfg.NoiseFile
	if cmd.Flags().Changed("noise-file") {
		effectiveNoise = flagNoise
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

func printStats(w io.Writer, result *pipeline.Result, showDetailed bool) {
	fs := result.Stats
	_, _ = fmt.Fprintln(w, "\n--- Filter Stats ---")
	_, _ = fmt.Fprintf(w, "Ingested:           %d\n", fs.TotalIngested)
	_, _ = fmt.Fprintf(w, "Dropped:            %d\n", fs.TotalDropped)
	_, _ = fmt.Fprintf(w, "Survived:           %d\n", fs.TotalSurvived)
	_, _ = fmt.Fprintf(w, "Unique signatures:  %d\n", fs.UniqueSignatures)
	_, _ = fmt.Fprintf(w, "Duration:           %dms\n", fs.FilterDurationMs)

	if showDetailed {
		ds := result.DetailedStats
		_, _ = fmt.Fprintln(w, "\n--- Detailed Breakdown ---")
		_, _ = fmt.Fprintf(w, "Dropped by level:       %d\n", ds.DroppedByLevel)
		_, _ = fmt.Fprintf(w, "Dropped by time window: %d\n", ds.DroppedByTimeWindow)
		_, _ = fmt.Fprintf(w, "Dropped by noise:       %d\n", ds.DroppedByNoise)
		_, _ = fmt.Fprintf(w, "Dropped by dedup:       %d\n", ds.DroppedByDedup)
		_, _ = fmt.Fprintf(w, "Dropped by auto-window: %d\n", ds.DroppedByAutoWindow)
		_, _ = fmt.Fprintf(w, "Spike events:           %d\n", ds.SpikeCount)
		if len(fs.SourceBreakdown) > 0 {
			_, _ = fmt.Fprintln(w, "\n--- Error Counts by Source ---")
			for src, cnt := range fs.SourceBreakdown {
				_, _ = fmt.Fprintf(w, "  %s: %d\n", src, cnt)
			}
		}
	}
}

func printStatsText(w io.Writer, fs types.FilterStats, result *pipeline.Result) {
	fileCount := fs.FileCount
	if fileCount == 0 {
		fileCount = len(fs.SourceBreakdown)
	}

	bytes := fs.BytesProcessed
	var bytesStr string
	switch {
	case bytes >= 1<<20:
		bytesStr = fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		bytesStr = fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	default:
		bytesStr = fmt.Sprintf("%d B", bytes)
	}

	_, _ = fmt.Fprintf(w, "Files:          %d\n", fileCount)
	if bytes > 0 {
		_, _ = fmt.Fprintf(w, "Bytes:          %s\n", bytesStr)
	}
	_, _ = fmt.Fprintf(w, "Ingested:           %s\n", render.FmtIntComma(fs.TotalIngested))
	_, _ = fmt.Fprintf(w, "Dropped:            %s\n", render.FmtIntComma(fs.TotalDropped))
	_, _ = fmt.Fprintf(w, "Survived:           %s\n", render.FmtIntComma(fs.TotalSurvived))
	_, _ = fmt.Fprintf(w, "Unique signatures:  %d\n", fs.UniqueSignatures)
	_, _ = fmt.Fprintf(w, "Duration:           %dms\n", result.Duration.Milliseconds())

	if !fs.TimeWindowStart.IsZero() {
		winStart := fs.TimeWindowStart.Format("2006-01-02 15:04:05")
		winEnd := fs.TimeWindowEnd.Format("15:04:05")
		if !fs.TimeWindowEnd.IsZero() && fs.TimeWindowEnd.Format("2006-01-02") != fs.TimeWindowStart.Format("2006-01-02") {
			winEnd = fs.TimeWindowEnd.Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(w, "Time window:    %s — %s\n", winStart, winEnd)
	}
}

func resolveProvider(providerName, model string) (analyze.Provider, error) {
	switch providerName {
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: OPENAI_API_KEY not set")
		}
		return analyze.NewOpenAI(key, model, "")
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: ANTHROPIC_API_KEY not set")
		}
		return analyze.NewAnthropic(key, model, "")
	case "ollama":
		return analyze.NewOllama("", model), nil
	default:
		return nil, fmt.Errorf("cmd: unknown AI provider: %q (valid: openai, anthropic, ollama)", providerName)
	}
}

// parseTimeFlag parses a relative duration ("2h") or ISO 8601 timestamp.
func parseTimeFlag(s string, now time.Time) (time.Time, error) {
	d, err := time.ParseDuration(s)
	if err == nil {
		return now.Add(-d), nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		var t time.Time
		if layout == time.RFC3339 {
			t, err = time.Parse(layout, s)
		} else {
			t, err = time.ParseInLocation(layout, s, time.Local)
		}
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as duration or timestamp", s)
}

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

// newProvider creates an LLM provider from a provider name and optional model override.
func newProvider(name, model string, timeout time.Duration) (analyze.Provider, error) {
	switch name {
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: OPENAI_API_KEY not set")
		}
		return analyze.NewOpenAI(key, model, "", timeout)
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: ANTHROPIC_API_KEY not set")
		}
		return analyze.NewAnthropic(key, model, "", timeout)
	case "gemini":
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: GEMINI_API_KEY not set")
		}
		return analyze.NewGemini(key, model, "", timeout)
	case "ollama":
		return analyze.NewOllama("", model, timeout), nil
	default:
		return nil, fmt.Errorf("cmd: unknown AI provider: %q (valid: openai, anthropic, gemini, ollama)", name)
	}
}

// newRenderer returns a renderer for the given format.
func newRenderer(format string) render.Renderer {
	switch format {
	case "json":
		return &render.JSONRenderer{}
	case "markdown":
		return &render.MarkdownRenderer{}
	default:
		return &render.TerminalRenderer{}
	}
}

func printStats(w io.Writer, result *pipeline.Result, ar *analyze.AnalysisResult, showDetailed bool) {
	fs := result.Stats
	_, _ = fmt.Fprintln(w, "\n--- Filter Stats ---")
	_, _ = fmt.Fprintf(w, "Ingested:           %d\n", fs.TotalIngested)
	_, _ = fmt.Fprintf(w, "Dropped:            %d\n", fs.TotalDropped)
	_, _ = fmt.Fprintf(w, "Survived:           %d\n", fs.TotalSurvived)
	_, _ = fmt.Fprintf(w, "Unique signatures:  %d\n", fs.UniqueSignatures)
	_, _ = fmt.Fprintf(w, "Duration:           %dms\n", fs.FilterDurationMs)
	if ar != nil {
		_, _ = fmt.Fprintf(w, "AI analysis:        %.1fs\n", ar.Duration.Seconds())
	}

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

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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

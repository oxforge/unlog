package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
)

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

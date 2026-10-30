package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
	"github.com/oxforge/unlog/internal/types"
)

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
		_, _ = fmt.Fprintf(w, "Merged duplicates:      %d\n", ds.DroppedByDedup)
		_, _ = fmt.Fprintf(w, "Bursts:                 %d\n", ds.BurstCount)

		if len(result.IngestStats) > 0 {
			_, _ = fmt.Fprintln(w, "\n--- Sources ---")
			// Sort source names for stable output.
			names := make([]string, 0, len(result.IngestStats))
			for name := range result.IngestStats {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				ss := result.IngestStats[name]
				_, _ = fmt.Fprintf(w, "  %s  format=%s entries=%d",
					name, ss.Format, ss.Entries)
				for _, lvl := range []types.Level{
					types.LevelFatal, types.LevelError, types.LevelWarn,
					types.LevelInfo, types.LevelDebug, types.LevelTrace,
				} {
					if c := ss.Levels[lvl]; c > 0 {
						_, _ = fmt.Fprintf(w, " %s=%d", lvl, c)
					}
				}
				if c := ss.Levels[types.LevelUnknown]; c > 0 {
					_, _ = fmt.Fprintf(w, " unknown=%d", c)
				}
				if ss.Unparsed > 0 {
					_, _ = fmt.Fprintf(w, " unparsed=%d", ss.Unparsed)
				}
				_, _ = fmt.Fprintln(w)
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

// parseTimeFlag parses a relative duration ("2h", before now) or a timestamp;
// a timestamp without a zone is read in loc, as log timestamps are.
func parseTimeFlag(s string, now time.Time, loc *time.Location) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as duration or timestamp", s)
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

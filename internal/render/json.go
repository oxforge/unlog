package render

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/types"
)

type JSONRenderer struct{}

func (r *JSONRenderer) Render(w io.Writer, opts Options) error {
	return RenderJSON(w, opts.Result, opts.Version)
}

// RenderJSON writes a complete AnalysisReport as indented JSON to w.
func RenderJSON(w io.Writer, result *pipeline.Result, version string) error {
	report := types.AnalysisReport{
		GeneratedAt:        time.Now().UTC(),
		UnlogVersion:       version,
		AnalysisDurationMs: result.Duration.Milliseconds(),
		Stats:              result.Stats,
		CompactedSummary:   result.Summary,
	}

	if report.Stats.SourceBreakdown == nil {
		report.Stats.SourceBreakdown = map[string]int64{}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("render: json encode: %w", err)
	}
	return nil
}

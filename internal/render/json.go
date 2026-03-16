package render

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/types"
)

type JSONRenderer struct{}

func (r *JSONRenderer) Render(w io.Writer, opts Options) error {
	return RenderJSON(w, opts.Result, opts.Analysis, opts.Version)
}

// RenderJSON writes a complete AnalysisReport as indented JSON to w.
func RenderJSON(w io.Writer, result *pipeline.Result, ar *analyze.AnalysisResult, version string) error {
	report := types.AnalysisReport{
		GeneratedAt:        time.Now().UTC(),
		UnlogVersion:       version,
		AnalysisDurationMs: result.Duration.Milliseconds(),
		Stats:              result.Stats,
		CompactedSummary:   result.Summary,
	}

	if ar != nil {
		report.Timeline = ar.Timeline
		report.RootCause = ar.RootCause
		report.Recommendations = ar.Recommendations
		report.ModelUsed = ar.ModelUsed
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("render: json encode: %w", err)
	}
	return nil
}

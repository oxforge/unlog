package render_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleResult() *pipeline.Result {
	return &pipeline.Result{
		Summary: "## Incident Overview\nDatabase connection pool exhausted.\n## Critical Errors\nERROR: pool timeout after 30s\nWARN: connection count high\nINFO: health check passed\n",
		Stats: types.FilterStats{
			TotalIngested:    1000,
			TotalDropped:     800,
			TotalSurvived:    200,
			UniqueSignatures: 15,
			FilterDurationMs: 50,
		},
		Duration: 200 * time.Millisecond,
	}
}

func sampleAnalysis() *analyze.AnalysisResult {
	return &analyze.AnalysisResult{
		Analysis:  "10:00 — DB connection pool saturated\n10:01 — Requests started timing out\nConnection leak in payment service caused pool exhaustion\n1. Add connection pool monitoring\n2. Set max connection lifetime",
		ModelUsed: "gpt-4o",
	}
}

func TestTerminalRenderNoAI(t *testing.T) {
	r := &render.TerminalRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result:  sampleResult(),
		NoColor: true,
		Version: "1.0.0",
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "## Incident Overview")
	assert.Contains(t, out, "## Critical Errors")
	assert.Contains(t, out, "Database connection pool exhausted.")
	// Stats are rendered by the cmd layer to stderr, not by the terminal renderer.
	assert.NotContains(t, out, "Filter Stats")
	// No ANSI escapes when NoColor is true.
	assert.NotContains(t, out, "\033[")
}

func TestTerminalRenderWithAI(t *testing.T) {
	r := &render.TerminalRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result:   sampleResult(),
		Analysis: sampleAnalysis(),
		NoColor:  true,
		Version:  "1.0.0",
	})
	require.NoError(t, err)

	out := buf.String()
	// AI mode should also render the compacted summary.
	assert.Contains(t, out, "## Incident Overview")
	assert.Contains(t, out, "--- Analysis ---")
	assert.Contains(t, out, "DB connection pool saturated")
	assert.Contains(t, out, "Connection leak")
	assert.Contains(t, out, "connection pool monitoring")
}

func TestTerminalRenderColored(t *testing.T) {
	r := &render.TerminalRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result:  sampleResult(),
		NoColor: false,
		Version: "1.0.0",
	})
	require.NoError(t, err)

	out := buf.String()
	// Should contain ANSI escape codes.
	assert.True(t, strings.Contains(out, "\033["), "expected ANSI escape codes in colored output")
	// Section headers should be bold cyan.
	assert.Contains(t, out, "\033[1;36m")
	// Level keywords should be colored.
	assert.Contains(t, out, "\033[31mERROR\033[0m")
	assert.Contains(t, out, "\033[33mWARN\033[0m")
	assert.Contains(t, out, "\033[32mINFO\033[0m")
}

func TestTerminalRenderNoColor(t *testing.T) {
	r := &render.TerminalRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result:   sampleResult(),
		Analysis: sampleAnalysis(),
		NoColor:  true,
		Version:  "1.0.0",
	})
	require.NoError(t, err)

	out := buf.String()
	assert.NotContains(t, out, "\033[", "expected no ANSI escape codes when NoColor is true")
	// Content should still be present.
	assert.Contains(t, out, "--- Analysis ---")
	assert.Contains(t, out, "DB connection pool saturated")
}

// failWriter is an io.Writer that always returns an error.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestTerminalRenderWriteError(t *testing.T) {
	r := &render.TerminalRenderer{}
	err := r.Render(failWriter{}, render.Options{
		Result:  sampleResult(),
		NoColor: true,
		Version: "1.0.0",
	})
	assert.Error(t, err, "should propagate write errors")
}

func TestMarkdownRenderWriteError(t *testing.T) {
	r := &render.MarkdownRenderer{}
	err := r.Render(failWriter{}, render.Options{
		Result:  sampleResult(),
		Version: "1.0.0",
	})
	assert.Error(t, err, "should propagate write errors")
}

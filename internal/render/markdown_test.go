package render_test

import (
	"bytes"
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

func TestMarkdownRenderNoAI(t *testing.T) {
	r := &render.MarkdownRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result: &pipeline.Result{
			Summary: "Database connection pool exhausted.",
			Stats: types.FilterStats{
				TotalIngested:    500,
				TotalDropped:     400,
				TotalSurvived:    100,
				UniqueSignatures: 8,
				FilterDurationMs: 30,
			},
			Duration: 100 * time.Millisecond,
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "# Incident Analysis")
	assert.Contains(t, out, "## Summary")
	assert.Contains(t, out, "## Statistics")
	assert.Contains(t, out, "Database connection pool exhausted.")
	// No AI sections.
	assert.NotContains(t, out, "## Timeline")
	assert.NotContains(t, out, "## Root Cause")
	assert.NotContains(t, out, "## Recommendations")
	// No model metadata.
	assert.NotContains(t, out, "**Model**")
}

func TestMarkdownRenderWithAI(t *testing.T) {
	r := &render.MarkdownRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result: &pipeline.Result{
			Summary: "Summary text here.",
			Stats: types.FilterStats{
				TotalIngested:    10000,
				TotalDropped:     8500,
				TotalSurvived:    1500,
				UniqueSignatures: 42,
				FilterDurationMs: 100,
			},
			Duration: 1230 * time.Millisecond,
		},
		Analysis: &analyze.AnalysisResult{
			Analysis:  "10:00 — Incident began\nConnection leak in service X\n1. Monitor pool usage",
			ModelUsed: "gpt-4o",
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "# Incident Analysis")
	assert.Contains(t, out, "## Analysis")
	assert.Contains(t, out, "10:00 — Incident began")
	assert.Contains(t, out, "Connection leak")
	assert.Contains(t, out, "Monitor pool usage")
	assert.Contains(t, out, "## Summary")
	assert.Contains(t, out, "## Statistics")
	assert.Contains(t, out, "**Model**: gpt-4o")
}

func TestMarkdownStatsTable(t *testing.T) {
	r := &render.MarkdownRenderer{}
	var buf bytes.Buffer

	err := r.Render(&buf, render.Options{
		Result: &pipeline.Result{
			Stats: types.FilterStats{
				TotalIngested:    10000,
				TotalDropped:     8500,
				TotalSurvived:    1500,
				UniqueSignatures: 42,
			},
			Duration: 1230 * time.Millisecond,
		},
		Version: "dev",
	})
	require.NoError(t, err)

	out := buf.String()
	// Verify valid markdown table structure.
	assert.Contains(t, out, "| Metric | Value |")
	assert.Contains(t, out, "|--------|-------|")
	assert.Contains(t, out, "| Ingested | 10,000 |")
	assert.Contains(t, out, "| Dropped | 8,500 |")
	assert.Contains(t, out, "| Survived | 1,500 |")
	assert.Contains(t, out, "| Unique signatures | 42 |")

	// Every table row should have consistent pipe count.
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "|") {
			assert.Equal(t, 3, strings.Count(line, "|"), "table row should have 3 pipes: %q", line)
		}
	}
}

package render_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderJSON(t *testing.T) {
	result := &pipeline.Result{
		Summary: "## Incident Overview\nDatabase connection pool exhausted.\n",
		Stats: types.FilterStats{
			TotalIngested:    100,
			TotalDropped:     80,
			TotalSurvived:    20,
			UniqueSignatures: 5,
			FilterDuration:   50 * time.Millisecond,
		},
		Duration: 123 * time.Millisecond,
	}

	var buf bytes.Buffer
	err := render.RenderJSON(&buf, result, nil, "1.0.0")
	require.NoError(t, err)

	// Output must be valid JSON.
	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report), "output must be valid JSON")

	assert.Equal(t, "1.0.0", report.UnlogVersion)
	assert.Equal(t, result.Summary, report.CompactedSummary)
	assert.Equal(t, result.Stats.TotalIngested, report.Stats.TotalIngested)
	assert.Equal(t, result.Stats.TotalSurvived, report.Stats.TotalSurvived)
	assert.Equal(t, result.Stats.UniqueSignatures, report.Stats.UniqueSignatures)
	assert.Equal(t, result.Duration, report.AnalysisDuration)
	assert.Empty(t, report.ModelUsed, "no AI result, model_used should be empty")
	assert.Empty(t, report.Timeline)
	assert.Empty(t, report.RootCause)
	assert.Empty(t, report.Recommendations)
	assert.False(t, report.GeneratedAt.IsZero(), "GeneratedAt should be set")
}

func TestRenderJSONEmptySummary(t *testing.T) {
	result := &pipeline.Result{
		Summary: "",
		Stats:   types.FilterStats{},
	}

	var buf bytes.Buffer
	err := render.RenderJSON(&buf, result, nil, "dev")
	require.NoError(t, err)

	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report))

	assert.Equal(t, "", report.CompactedSummary, "empty summary should produce empty compacted_summary field")
	assert.Equal(t, "dev", report.UnlogVersion)
}

func TestRenderJSONIndented(t *testing.T) {
	result := &pipeline.Result{Summary: "test", Stats: types.FilterStats{}}
	var buf bytes.Buffer
	require.NoError(t, render.RenderJSON(&buf, result, nil, "dev"))

	// Verify indented output (contains newlines and spaces).
	out := buf.String()
	assert.Contains(t, out, "\n  ", "JSON output should be indented with 2 spaces")
}

func TestJSONRendererInterface(t *testing.T) {
	result := &pipeline.Result{
		Summary: "## Incident Overview\nTest summary.\n",
		Stats: types.FilterStats{
			TotalIngested: 50,
			TotalSurvived: 10,
			TotalDropped:  40,
		},
		Duration: 100 * time.Millisecond,
	}
	ar := &analyze.AnalysisResult{
		Timeline:        "timeline text",
		RootCause:       "root cause text",
		Recommendations: "recommendations text",
		ModelUsed:       "gpt-4o",
	}

	var r render.Renderer = &render.JSONRenderer{}
	var buf bytes.Buffer
	err := r.Render(&buf, render.Options{
		Result:   result,
		Analysis: ar,
		Version:  "1.0.0",
	})
	require.NoError(t, err)

	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report))

	assert.Equal(t, "timeline text", report.Timeline)
	assert.Equal(t, "root cause text", report.RootCause)
	assert.Equal(t, "recommendations text", report.Recommendations)
	assert.Equal(t, "gpt-4o", report.ModelUsed)
	assert.Equal(t, "1.0.0", report.UnlogVersion)
}

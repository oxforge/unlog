package render_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
	"github.com/oxforge/unlog/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllRenderersImplementInterface(t *testing.T) {
	var _ render.Renderer = &render.JSONRenderer{}
	var _ render.Renderer = &render.TerminalRenderer{}
	var _ render.Renderer = &render.MarkdownRenderer{}
}

func makeResult() *pipeline.Result {
	return &pipeline.Result{
		Summary: "## Overview\nDatabase connection pool exhausted.\n",
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

func TestRenderersRenderAResult(t *testing.T) {
	renderers := map[string]render.Renderer{
		"json":     &render.JSONRenderer{},
		"terminal": &render.TerminalRenderer{},
		"markdown": &render.MarkdownRenderer{},
	}

	for name, r := range renderers {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			err := r.Render(&buf, render.Options{
				Result:  makeResult(),
				NoColor: true,
				Version: "1.0.0",
			})
			require.NoError(t, err)
			assert.NotEmpty(t, buf.String(), "renderer %s produced empty output", name)
		})
	}
}

func TestRenderersOutputNonEmpty(t *testing.T) {
	// Minimal result — verify no renderer panics or returns empty.
	// Terminal renderer is excluded: with no summary it
	// legitimately produces empty output (stats go to stderr in the cmd layer).
	result := &pipeline.Result{
		Stats:    types.FilterStats{},
		Duration: time.Millisecond,
	}

	renderers := map[string]render.Renderer{
		"json":     &render.JSONRenderer{},
		"markdown": &render.MarkdownRenderer{},
	}

	for name, r := range renderers {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			err := r.Render(&buf, render.Options{
				Result:  result,
				NoColor: true,
				Version: "dev",
			})
			require.NoError(t, err)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestFmtIntComma(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{10000, "10,000"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, render.FmtIntComma(tt.input), "FmtIntComma(%d)", tt.input)
	}
}

func TestMarkdownNoANSICodes(t *testing.T) {
	var buf bytes.Buffer
	r := &render.MarkdownRenderer{}
	err := r.Render(&buf, render.Options{
		Result:  makeResult(),
		NoColor: false, // even with color "enabled", markdown should never emit ANSI
		Version: "1.0.0",
	})
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "\033[")
}

func TestJSONAlwaysValid(t *testing.T) {
	// JSON renderer must produce valid JSON regardless of NoColor setting.
	for _, nc := range []bool{true, false} {
		var buf bytes.Buffer
		r := &render.JSONRenderer{}
		err := r.Render(&buf, render.Options{
			Result:  makeResult(),
			NoColor: nc,
			Version: "1.0.0",
		})
		require.NoError(t, err)
		assert.True(t, json.Valid(buf.Bytes()), "JSON output must be valid (noColor=%v)", nc)
		// Verify it contains no trailing garbage after the JSON object.
		trimmed := strings.TrimSpace(buf.String())
		assert.True(t, strings.HasPrefix(trimmed, "{"), "JSON should start with {")
		assert.True(t, strings.HasSuffix(trimmed, "}"), "JSON should end with }")
	}
}

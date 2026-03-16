package pipeline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testdataDir returns the absolute path to the testdata/incidents directory
// relative to this file's package root.
func testdataDir(t *testing.T, sub string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// This file lives at internal/pipeline/; testdata is at the repo root.
	root := filepath.Join(filepath.Dir(file), "..", "..")
	return filepath.Join(root, "testdata", "incidents", sub)
}

func TestIntegrationDBConnection(t *testing.T) {
	dir := testdataDir(t, "db_connection")

	p := pipeline.New(pipeline.Options{})
	result, err := p.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Stats sanity checks.
	assert.Greater(t, result.Stats.TotalIngested, int64(0), "should have ingested log entries")
	assert.Greater(t, result.Stats.TotalSurvived, int64(0), "some entries should survive filtering")
	assert.Less(t, result.Stats.TotalSurvived, result.Stats.TotalIngested, "filter should drop some entries")

	// Summary content.
	assert.NotEmpty(t, result.Summary, "summary should be non-empty")

	lowerSummary := strings.ToLower(result.Summary)
	assert.True(t,
		strings.Contains(lowerSummary, "connection") || strings.Contains(lowerSummary, "database"),
		"summary should mention 'connection' or 'database', got: %s", result.Summary)

	// Token budget: summary should be well under 8192 tokens (len/3.5 heuristic).
	estimatedTokens := len(result.Summary) * 2 / 7
	assert.Less(t, estimatedTokens, 8192, "summary should be within token budget")
}

func TestIntegrationDeployFailure(t *testing.T) {
	dir := testdataDir(t, "deploy_failure")

	p := pipeline.New(pipeline.Options{})
	result, err := p.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Stats.
	assert.Greater(t, result.Stats.TotalIngested, int64(0))
	assert.Greater(t, result.Stats.TotalSurvived, int64(0))

	// Summary should mention deployment-related content.
	assert.NotEmpty(t, result.Summary)
	lowerSummary := strings.ToLower(result.Summary)
	assert.True(t,
		strings.Contains(lowerSummary, "deploy") ||
			strings.Contains(lowerSummary, "rollback") ||
			strings.Contains(lowerSummary, "health"),
		"summary should mention deployment-related content, got: %s", result.Summary)
}

func TestIntegrationJSONOutput(t *testing.T) {
	dir := testdataDir(t, "db_connection")

	p := pipeline.New(pipeline.Options{})
	result, err := p.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NotNil(t, result)

	var buf bytes.Buffer
	err = render.RenderJSON(&buf, result, nil, "test-version")
	require.NoError(t, err, "RenderJSON should not fail")

	// Must be valid JSON.
	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal(buf.Bytes(), &report), "output must unmarshal into AnalysisReport")

	assert.Equal(t, "test-version", report.UnlogVersion)
	assert.Equal(t, result.Summary, report.CompactedSummary)
	assert.Equal(t, result.Stats.TotalIngested, report.Stats.TotalIngested)
	assert.False(t, report.GeneratedAt.IsZero())
}

func TestIntegrationStatsOnly(t *testing.T) {
	dir := testdataDir(t, "db_connection")

	p := pipeline.New(pipeline.Options{
		StopAfter: pipeline.StopAfterFilter,
	})
	result, err := p.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Summary must be empty when stopping after filter.
	assert.Empty(t, result.Summary, "summary should be empty for StopAfterFilter")

	// Stats must be populated.
	assert.Greater(t, result.Stats.TotalIngested, int64(0), "stats should be populated")
	assert.GreaterOrEqual(t, result.Stats.TotalSurvived, int64(0))
}

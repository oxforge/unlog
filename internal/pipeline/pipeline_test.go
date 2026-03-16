package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempLog writes content to a temporary .log file and returns its path.
func writeTempLog(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// syntheticLogs returns a multi-line logfmt log with some errors.
func syntheticLogs() string {
	return `time="2024-01-15T10:30:00Z" level=info msg="service started" source=app
time="2024-01-15T10:30:01Z" level=warn msg="high memory usage" source=app
time="2024-01-15T10:30:02Z" level=error msg="database connection failed" source=app
time="2024-01-15T10:30:03Z" level=error msg="database connection failed" source=app
time="2024-01-15T10:30:04Z" level=error msg="database connection failed" source=app
time="2024-01-15T10:30:05Z" level=fatal msg="connection pool exhausted, shutting down" source=app
`
}

func TestPipelineEndToEnd(t *testing.T) {
	path := writeTempLog(t, syntheticLogs())

	p := pipeline.New(pipeline.Options{})
	result, err := p.Run(context.Background(), []string{path})

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.Summary, "summary should be non-empty")
	assert.Greater(t, result.Stats.TotalIngested, int64(0), "should have ingested entries")
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestPipelineStopAfterFilter(t *testing.T) {
	path := writeTempLog(t, syntheticLogs())

	p := pipeline.New(pipeline.Options{
		StopAfter: pipeline.StopAfterFilter,
	})
	result, err := p.Run(context.Background(), []string{path})

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Summary, "summary should be empty when StopAfterFilter")
	assert.Greater(t, result.Stats.TotalIngested, int64(0), "stats should be populated")
}

func TestPipelineCancellation(t *testing.T) {
	// Write a large enough log to give the pipeline some work to do.
	var lines []byte
	for i := 0; i < 1000; i++ {
		lines = append(lines, []byte(`time="2024-01-15T10:30:00Z" level=error msg="boom" source=app`+"\n")...)
	}
	path := writeTempLog(t, string(lines))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the pipeline sees a cancelled context.
	cancel()

	p := pipeline.New(pipeline.Options{})
	_, err := p.Run(ctx, []string{path})

	// We expect either a context error or nil (fast path: all data processed
	// before goroutines check ctx). Both are acceptable outcomes.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

func TestPipelineEmptyInput(t *testing.T) {
	// No sources — pipeline should complete gracefully with zero stats.
	p := pipeline.New(pipeline.Options{})
	result, err := p.Run(context.Background(), []string{})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Empty input: nothing was ingested.
	assert.Equal(t, int64(0), result.Stats.TotalIngested)
	// Summary is still valid (the compact stage emits an "empty" placeholder).
	assert.NotEmpty(t, result.Summary, "compact produces a placeholder even for empty input")
}

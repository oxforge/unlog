package enrich

import (
	"context"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnricher_Integration(t *testing.T) {
	input := make(chan types.FilteredEntry, 10)
	output := make(chan types.EnrichedEntry, 10)

	e := NewEnricher(input, output, DefaultOptions())

	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Send entries that exercise all enrichment components.
	entries := []types.FilteredEntry{
		{
			LogEntry: types.LogEntry{
				Timestamp:  base,
				Level:      types.LevelError,
				Source:     "api-svc",
				Message:    "HTTP/1.1 500 Internal Server Error",
				LineNumber: 1,
				Metadata:   map[string]string{"trace_id": "abc-123"},
			},
		},
		{
			LogEntry: types.LogEntry{
				Timestamp:  base.Add(1 * time.Second),
				Level:      types.LevelInfo,
				Source:     "deploy-svc",
				Message:    "Starting application on port 8080",
				LineNumber: 2,
			},
		},
		{
			LogEntry: types.LogEntry{
				Timestamp:  base.Add(2 * time.Second),
				Level:      types.LevelError,
				Source:     "db-svc",
				Message:    "Error: ConnectionRefused to primary",
				LineNumber: 3,
			},
		},
	}

	for _, entry := range entries {
		input <- entry
	}
	close(input)

	err := e.Run(context.Background())
	require.NoError(t, err)

	// Collect output.
	var results []types.EnrichedEntry
	for entry := range output {
		results = append(results, entry)
	}

	require.Len(t, results, 3)

	// Entry 0: HTTP status from regex, trace ID from metadata.
	assert.Equal(t, 500, results[0].HTTPStatus)
	assert.Equal(t, "abc-123", results[0].TraceID)

	// Entry 1: deployment detected.
	assert.True(t, results[1].IsDeployment)

	// Entry 2: error type extracted.
	assert.Equal(t, "ConnectionRefused", results[2].ErrorType)

	// Entries 1 and 2 should be correlated with previous sources (within 5s).
	assert.NotNil(t, results[1].CorrelatedWith)
	assert.NotNil(t, results[2].CorrelatedWith)
}

func TestEnricher_ContextCancellation(t *testing.T) {
	input := make(chan types.FilteredEntry, 10)
	output := make(chan types.EnrichedEntry, 10)

	e := NewEnricher(input, output, DefaultOptions())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := e.Run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEnricher_EmptyInput(t *testing.T) {
	input := make(chan types.FilteredEntry)
	output := make(chan types.EnrichedEntry, 10)

	e := NewEnricher(input, output, DefaultOptions())

	close(input)
	err := e.Run(context.Background())
	require.NoError(t, err)

	// Output should be closed with no entries.
	_, ok := <-output
	assert.False(t, ok)
}

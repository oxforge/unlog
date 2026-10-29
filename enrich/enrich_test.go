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

	// Only errors correlate: the INFO deploy line is not tagged; the api and
	// db errors started 2s apart, and each is tagged with the other.
	assert.Equal(t, []string{"db-svc"}, results[0].CorrelatedWith)
	assert.Nil(t, results[1].CorrelatedWith)
	assert.Equal(t, []string{"api-svc"}, results[2].CorrelatedWith)
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

// The pipeline passes zero options; the 5s correlation window must still apply.
func TestEnricher_ZeroOptionsUseTheDefaultWindow(t *testing.T) {
	input := make(chan types.FilteredEntry, 2)
	output := make(chan types.EnrichedEntry, 2)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	input <- types.FilteredEntry{LogEntry: types.LogEntry{Timestamp: base, Level: types.LevelError, Source: "api", Message: "request failed"}}
	input <- types.FilteredEntry{LogEntry: types.LogEntry{Timestamp: base.Add(2 * time.Second), Level: types.LevelError, Source: "db", Message: "disk full"}}
	close(input)

	require.NoError(t, NewEnricher(input, output, EnrichOptions{}).Run(context.Background()))

	assert.Equal(t, []string{"db"}, (<-output).CorrelatedWith)
	assert.Equal(t, []string{"api"}, (<-output).CorrelatedWith)
}

// An entry in two chains is linked to both, so neither is shown without it.
func TestEnricher_LinksAnEntryToEveryChainItIsIn(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	lines := []string{
		"DNS lookup failed for payments.internal",
		"dial tcp 10.0.0.5:443: connect: ECONNREFUSED",
		"circuit breaker open for payments",
	}
	input := make(chan types.FilteredEntry, len(lines))
	output := make(chan types.EnrichedEntry, len(lines))
	for i, msg := range lines {
		input <- types.FilteredEntry{LogEntry: types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second), Level: types.LevelError, Source: "app", Message: msg, LineNumber: int64(i + 1)}}
	}
	close(input)

	require.NoError(t, NewEnricher(input, output, DefaultOptions()).Run(context.Background()))

	var results []types.EnrichedEntry
	for e := range output {
		results = append(results, e)
	}
	require.Len(t, results, 3)
	assert.Equal(t, []types.ChainLink{
		{ID: "dns-failure", Step: 2, Steps: 3},
		{ID: "circuit-breaker", Step: 1, Steps: 3},
	}, results[1].Chains)
	assert.Equal(t, "dns-failure", results[1].ChainID, "ChainID repeats the first link")
}

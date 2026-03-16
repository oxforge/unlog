package filter

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndToEnd(t *testing.T) {
	// 17 entries: 10 info, 5 error, 1 noise at error level, 1 unknown level.
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	input := make(chan types.LogEntry, 20)
	output := make(chan types.FilteredEntry, 20)

	// 10 INFO entries — should be dropped by level filter (default minLevel=Warn).
	for i := 0; i < 10; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     types.LevelInfo,
			Source:    "app",
			Message:   "info message",
		}
	}

	// 5 unique ERROR entries — should survive.
	for i := 0; i < 5; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(10+i) * time.Second),
			Level:     types.LevelError,
			Source:    "app",
			Message:   fmt.Sprintf("error number %d", i),
		}
	}

	// 1 noise at ERROR level — should be dropped by noise filter.
	input <- types.LogEntry{
		Timestamp: base.Add(16 * time.Second),
		Level:     types.LevelError,
		Source:    "app",
		Message:   "GET /healthz returned 200",
	}

	// 1 unknown level — should be kept (LevelUnknown passes level filter).
	input <- types.LogEntry{
		Timestamp: base.Add(17 * time.Second),
		Level:     types.LevelUnknown,
		Source:    "sidecar",
		Message:   "unknown level message",
	}

	close(input)

	opts := DefaultFilterOptions()
	opts.Workers = 2
	opts.AutoWindow = false // disable auto-window for this test

	p := NewFilterPipeline(input, output, opts, nil)

	fs, detailed, err := p.Run(context.Background())
	require.NoError(t, err)

	// Collect output.
	var results []types.FilteredEntry
	for fe := range output {
		results = append(results, fe)
	}

	// Stats invariant: TotalIngested == TotalDropped + TotalSurvived.
	assert.Equal(t, fs.TotalIngested, fs.TotalDropped+fs.TotalSurvived,
		"invariant: ingested == dropped + survived")
	assert.Equal(t, int64(17), fs.TotalIngested)

	// 10 info dropped + 1 noise dropped = 11 dropped.
	assert.Equal(t, int64(11), fs.TotalDropped)
	assert.Equal(t, int64(6), fs.TotalSurvived) // 5 errors + 1 unknown

	// Detailed stats.
	assert.Equal(t, int64(10), detailed.DroppedByLevel)
	assert.Equal(t, int64(1), detailed.DroppedByNoise)

	// Results should contain 6 real entries (may also have summaries).
	// Count non-summary results: those are entries from survivors.
	realCount := 0
	for _, r := range results {
		if r.OccurrenceCount <= opts.MaxDuplicates {
			realCount++
		}
	}
	assert.Equal(t, 6, realCount)
}

func TestSortedOutput(t *testing.T) {
	// 10 unique error entries in reverse chronological order.
	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	input := make(chan types.LogEntry, 20)
	output := make(chan types.FilteredEntry, 20)

	// Each message must have a distinct signature (no numbers that get normalized).
	messages := []string{
		"database connection refused",
		"disk space exhausted on primary",
		"authentication token expired",
		"circuit breaker opened for payments",
		"TLS certificate validation failed",
		"DNS resolution timeout for api gateway",
		"memory allocation failure detected",
		"queue consumer lag exceeding threshold",
		"upstream service returned bad gateway",
		"configuration reload triggered by watchdog",
	}
	for i := 9; i >= 0; i-- {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     types.LevelError,
			Source:    "svc",
			Message:   messages[i],
		}
	}
	close(input)

	opts := DefaultFilterOptions()
	opts.Workers = 4
	opts.AutoWindow = false

	p := NewFilterPipeline(input, output, opts, nil)

	_, _, err := p.Run(context.Background())
	require.NoError(t, err)

	var results []types.FilteredEntry
	for fe := range output {
		results = append(results, fe)
	}

	// Filter out any dedup summaries for sorting check.
	var real []types.FilteredEntry
	for _, r := range results {
		if r.OccurrenceCount <= opts.MaxDuplicates {
			real = append(real, r)
		}
	}

	require.Len(t, real, 10)
	assert.True(t, sort.SliceIsSorted(real, func(i, j int) bool {
		return real[i].Timestamp.Before(real[j].Timestamp)
	}), "output should be sorted by timestamp")
}

func TestContextCancellation(t *testing.T) {
	input := make(chan types.LogEntry, 100)
	output := make(chan types.FilteredEntry, 100)

	// Feed some entries but don't close the channel — simulate ongoing input.
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     types.LevelError,
			Source:    "app",
			Message:   fmt.Sprintf("error %d", i),
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	opts := DefaultFilterOptions()
	opts.Workers = 2
	opts.AutoWindow = false

	p := NewFilterPipeline(input, output, opts, nil)

	done := make(chan struct{})
	var runErr error
	go func() {
		_, _, runErr = p.Run(ctx)
		close(done)
	}()

	// Give workers a moment to start processing, then cancel.
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Must not hang. Wait with timeout.
	select {
	case <-done:
		// Success — Run returned.
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation — deadlock")
	}

	// Error should be context.Canceled or nil.
	if runErr != nil {
		assert.ErrorIs(t, runErr, context.Canceled)
	}
}

func TestEmptyInput(t *testing.T) {
	input := make(chan types.LogEntry)
	output := make(chan types.FilteredEntry, 10)

	close(input) // immediately closed — zero entries

	opts := DefaultFilterOptions()
	opts.Workers = 2
	opts.AutoWindow = false

	p := NewFilterPipeline(input, output, opts, nil)
	fs, _, err := p.Run(context.Background())
	require.NoError(t, err)

	var results []types.FilteredEntry
	for fe := range output {
		results = append(results, fe)
	}

	assert.Empty(t, results)
	assert.Equal(t, int64(0), fs.TotalIngested)
	assert.Equal(t, int64(0), fs.TotalDropped)
	assert.Equal(t, int64(0), fs.TotalSurvived)
}

func TestMaxSurvivorsOverflow(t *testing.T) {
	input := make(chan types.LogEntry, 200)
	output := make(chan types.FilteredEntry, 200)

	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Feed 100 unique error entries — more than MaxSurvivors=10
	for i := 0; i < 100; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     types.LevelError,
			Source:    "app",
			Message:   fmt.Sprintf("unique error message number %d in test", i),
		}
	}
	close(input)

	opts := DefaultFilterOptions()
	opts.Workers = 2
	opts.MaxSurvivors = 10 // very low cap to trigger overflow
	opts.AutoWindow = false
	opts.MaxDuplicates = 100 // high so dedup doesn't interfere

	p := NewFilterPipeline(input, output, opts, nil)
	fs, _, err := p.Run(context.Background())
	require.NoError(t, err)

	var results []types.FilteredEntry
	for fe := range output {
		results = append(results, fe)
	}

	// All 100 entries should reach output (either buffered or streamed)
	assert.Equal(t, int64(100), fs.TotalIngested)
	assert.Greater(t, len(results), 0)
	// In overflow mode, some entries were streamed directly and aren't counted
	// in survivors slice, so TotalSurvived only counts buffered ones.
	// The key assertion is that all entries made it to output.
	assert.Equal(t, 100, len(results), "all entries should reach output despite overflow")
}

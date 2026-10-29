package filter

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
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

	// 1 noise at WARN level — should be dropped by noise filter.
	input <- types.LogEntry{
		Timestamp: base.Add(16 * time.Second),
		Level:     types.LevelWarn,
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

	// 17 ingested; 10 info + 1 noise dropped; of the 6 that pass, the 5
	// "error number N" lines share one signature and merge into one record
	// (4 merged), plus the sidecar line: 2 survive, 15 dropped.
	assert.Equal(t, int64(15), fs.TotalDropped)
	assert.Equal(t, int64(2), fs.TotalSurvived)

	// Detailed stats.
	assert.Equal(t, int64(10), detailed.DroppedByLevel)
	assert.Equal(t, int64(1), detailed.DroppedByNoise)
	assert.Equal(t, int64(4), detailed.DroppedByDedup)

	require.Len(t, results, 2)
	assert.Equal(t, 5, results[0].OccurrenceCount)
	assert.Equal(t, "error number 0", results[0].Message)
	assert.Equal(t, 1, results[1].OccurrenceCount)
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

	p := NewFilterPipeline(input, output, opts, nil)

	_, _, err := p.Run(context.Background())
	require.NoError(t, err)

	var results []types.FilteredEntry
	for fe := range output {
		results = append(results, fe)
	}

	require.Len(t, results, 10)
	assert.True(t, sort.SliceIsSorted(results, func(i, j int) bool {
		return results[i].Timestamp.Before(results[j].Timestamp)
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

func TestTimeWindowIgnoresEntriesWithoutTimestamps(t *testing.T) {
	input := make(chan types.LogEntry, 3)
	output := make(chan types.FilteredEntry, 10)
	first := time.Date(2024, 1, 15, 10, 31, 0, 0, time.UTC)
	last := time.Date(2024, 1, 15, 10, 32, 25, 0, time.UTC)
	input <- types.LogEntry{Level: types.LevelError, Source: "db.log", Message: "undated failure"}
	input <- types.LogEntry{Timestamp: first, Level: types.LevelError, Source: "app.log", Message: "request failed"}
	input <- types.LogEntry{Timestamp: last, Level: types.LevelFatal, Source: "app.log", Message: "shutting down"}
	close(input)

	opts := DefaultFilterOptions()
	fs, _, err := NewFilterPipeline(input, output, opts, nil).Run(context.Background())
	require.NoError(t, err)
	for range output {
	}

	assert.True(t, first.Equal(fs.TimeWindowStart), "start: got %s", fs.TimeWindowStart)
	assert.True(t, last.Equal(fs.TimeWindowEnd), "end: got %s", fs.TimeWindowEnd)
}

func TestFilterStatsCountMergedDuplicates(t *testing.T) {
	input := make(chan types.LogEntry, 100)
	output := make(chan types.FilteredEntry, 100)
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		input <- types.LogEntry{Timestamp: base, Level: types.LevelInfo, Source: "app", Message: "started"}
	}
	for i := 0; i < 30; i++ {
		input <- types.LogEntry{Timestamp: base.Add(time.Duration(i) * time.Second), Level: types.LevelError,
			Source: "app", Message: "upstream timeout", LineNumber: int64(i + 1)}
	}
	input <- types.LogEntry{Timestamp: base.Add(30 * time.Second), Level: types.LevelFatal,
		Source: "app", Message: "out of memory", LineNumber: 31}
	for i := 0; i < 5; i++ {
		input <- types.LogEntry{Timestamp: base.Add(time.Duration(i) * time.Second), Level: types.LevelError,
			Source: "db", Message: "deadlock detected", LineNumber: int64(i + 1)}
	}
	close(input)

	fs, detailed, err := NewFilterPipeline(input, output, DefaultFilterOptions(), nil).Run(context.Background())
	require.NoError(t, err)
	for range output {
	}

	assert.Equal(t, int64(46), fs.TotalIngested)
	assert.Equal(t, int64(10), detailed.DroppedByLevel)
	assert.Equal(t, int64(3), fs.TotalSurvived)
	assert.Equal(t, int64(33), detailed.DroppedByDedup)
	assert.Equal(t, int64(43), fs.TotalDropped)
	assert.Equal(t, map[string]int64{"app": 31, "db": 5}, fs.SourceBreakdown)
	assert.True(t, base.Equal(fs.TimeWindowStart), "start: %s", fs.TimeWindowStart)
	assert.True(t, base.Add(30*time.Second).Equal(fs.TimeWindowEnd), "end: %s", fs.TimeWindowEnd)
}

func TestFilterOutputIsDeterministic(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []string{"upstream timeout for order %d", "disk %d%% full", "cache miss for key %d",
		"auth rejected for user %d", "queue lag at %d ms"}
	sources := []string{"a.log", "b.log", "c.log"}
	var all []types.LogEntry
	for i := 0; i < 3500; i++ {
		lvl := types.LevelError
		if i%7 == 0 {
			lvl = types.LevelWarn
		}
		all = append(all, types.LogEntry{
			Timestamp:  base.Add(time.Duration(i/2) * time.Second),
			Level:      lvl,
			Source:     sources[i%3],
			Message:    fmt.Sprintf(msgs[i%5], i),
			LineNumber: int64(i),
		})
	}

	run := func(seed int64) []types.FilteredEntry {
		entries := append([]types.LogEntry(nil), all...)
		rand.New(rand.NewSource(seed)).Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
		input := make(chan types.LogEntry, len(entries))
		output := make(chan types.FilteredEntry, len(entries))
		for _, e := range entries {
			input <- e
		}
		close(input)
		opts := DefaultFilterOptions()
		opts.Workers = 8
		_, _, err := NewFilterPipeline(input, output, opts, nil).Run(context.Background())
		require.NoError(t, err)
		var out []types.FilteredEntry
		for fe := range output {
			out = append(out, fe)
		}
		return out
	}

	first := run(0)
	require.NotEmpty(t, first)
	for seed := int64(1); seed < 20; seed++ {
		require.Equal(t, first, run(seed), "run %d differs", seed)
	}
}

func TestFilterPipelineReportsBursts(t *testing.T) {
	input := make(chan types.LogEntry, 1000)
	output := make(chan types.FilteredEntry, 1000)
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		input <- types.LogEntry{Timestamp: base.Add(time.Duration(i) * 10 * time.Second), Level: types.LevelError,
			Source: "same.log", Message: fmt.Sprintf("cache miss for key user:%d", i)}
	}
	for i := 0; i < 400; i++ {
		input <- types.LogEntry{Timestamp: base.Add(5*time.Minute + time.Duration(i)*25*time.Millisecond),
			Level: types.LevelError, Source: "same.log",
			Message: fmt.Sprintf("payment gateway refused connection from 10.0.3.%d", i)}
	}
	close(input)

	p := NewFilterPipeline(input, output, DefaultFilterOptions(), nil)
	_, detailed, err := p.Run(context.Background())

	require.NoError(t, err)
	assert.Len(t, p.Bursts(), 1)
	assert.Equal(t, int64(1), detailed.BurstCount)
	var spikes int
	for e := range output {
		if e.IsSpike {
			spikes++
		}
	}
	assert.Equal(t, 2, spikes)
}

func TestUndatedEntriesNeverFormABurst(t *testing.T) {
	input := make(chan types.LogEntry, 1000)
	output := make(chan types.FilteredEntry, 1000)
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 400; i++ {
		input <- types.LogEntry{Timestamp: base.Add(5*time.Minute + time.Duration(i)*25*time.Millisecond),
			Level: types.LevelError, Source: "same.log",
			Message: fmt.Sprintf("payment gateway refused connection from 10.0.3.%d", i)}
	}
	input <- types.LogEntry{Timestamp: base, Level: types.LevelError, Source: "same.log", Message: "cache miss for key user:0"}
	input <- types.LogEntry{Timestamp: base.Add(time.Hour), Level: types.LevelError, Source: "same.log", Message: "cache miss for key user:1"}
	for i := 0; i < 25; i++ {
		input <- types.LogEntry{Level: types.LevelError, Source: "same.log", Message: fmt.Sprintf("worker %d crashed", i)}
	}
	close(input)

	p := NewFilterPipeline(input, output, DefaultFilterOptions(), nil)
	_, _, err := p.Run(context.Background())
	require.NoError(t, err)

	require.Len(t, p.Bursts(), 1)
	assert.Equal(t, 2024, p.Bursts()[0].Start.Year())
	for e := range output {
		if strings.HasPrefix(e.Message, "worker") {
			assert.False(t, e.IsSpike, "undated record marked as spike")
			assert.Equal(t, 25, e.OccurrenceCount)
		}
	}
}

func TestKeepAtAnyLevelPassesTheLevelFilter(t *testing.T) {
	input := make(chan types.LogEntry, 3)
	output := make(chan types.FilteredEntry, 3)
	at := time.Date(2024, 1, 15, 14, 1, 55, 0, time.UTC)
	input <- types.LogEntry{Timestamp: at, Level: types.LevelInfo, Source: "deploy.log", Message: "deployment started: app to 3.3.0"}
	input <- types.LogEntry{Timestamp: at.Add(time.Second), Level: types.LevelInfo, Source: "deploy.log", Message: "pod app-pod-1 updated"}
	input <- types.LogEntry{Timestamp: at.Add(2 * time.Second), Level: types.LevelError, Source: "app.log", Message: "health check failed"}
	close(input)

	opts := DefaultFilterOptions()
	opts.KeepAtAnyLevel = func(e types.LogEntry) bool { return strings.HasPrefix(e.Message, "deployment started") }
	_, detailed, err := NewFilterPipeline(input, output, opts, nil).Run(context.Background())
	require.NoError(t, err)

	var messages []string
	for e := range output {
		messages = append(messages, e.Message)
	}
	assert.Equal(t, []string{"deployment started: app to 3.3.0", "health check failed"}, messages)
	assert.Equal(t, int64(1), detailed.DroppedByLevel)
}

// Lines kept at any level (deployments) are merged only when identical:
// what tells two of them apart, a version, is what matters.
func TestKeepAtAnyLevelLinesMergeOnlyWhenIdentical(t *testing.T) {
	at := time.Date(2024, 1, 15, 8, 0, 0, 0, time.UTC)
	lines := []types.LogEntry{
		{Timestamp: at, Level: types.LevelInfo, Message: "deployment started: checkout to version 3.1.0"},
		{Timestamp: at.Add(time.Hour), Level: types.LevelInfo, Message: "deployment started: checkout to version 3.1.1"},
		{Timestamp: at.Add(3 * time.Hour), Level: types.LevelInfo, Message: "deployment started: checkout to version 4.0.0"},
		{Timestamp: at.Add(3*time.Hour + time.Minute), Level: types.LevelInfo, Message: "deployment started: checkout to version 4.0.0"},
		{Timestamp: at.Add(3*time.Hour + 2*time.Minute), Level: types.LevelError, Message: "payment 1001 declined"},
		{Timestamp: at.Add(3*time.Hour + 3*time.Minute), Level: types.LevelError, Message: "payment 1002 declined"},
	}
	input := make(chan types.LogEntry, len(lines))
	output := make(chan types.FilteredEntry, len(lines))
	for i, l := range lines {
		l.Source, l.LineNumber = "app.log", int64(i+1)
		input <- l
	}
	close(input)

	opts := DefaultFilterOptions()
	opts.KeepAtAnyLevel = func(e types.LogEntry) bool { return strings.HasPrefix(e.Message, "deployment started") }
	_, _, err := NewFilterPipeline(input, output, opts, nil).Run(context.Background())
	require.NoError(t, err)

	counts := map[string]int{}
	for e := range output {
		counts[e.Message] = e.OccurrenceCount
	}
	assert.Equal(t, map[string]int{
		"deployment started: checkout to version 3.1.0": 1,
		"deployment started: checkout to version 3.1.1": 1,
		"deployment started: checkout to version 4.0.0": 2,
		"payment 1001 declined":                         2, // errors still merge by signature
	}, counts)
}

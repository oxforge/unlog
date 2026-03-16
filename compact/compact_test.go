package compact

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEntry is a test helper that builds an EnrichedEntry with sane defaults.
func makeEntry(level types.Level, msg, source string, ts time.Time) types.EnrichedEntry {
	return types.EnrichedEntry{
		FilteredEntry: types.FilteredEntry{
			LogEntry: types.LogEntry{
				Timestamp: ts,
				Level:     level,
				Source:    source,
				Message:   msg,
			},
			OccurrenceCount: 1,
		},
	}
}

// sendAndClose sends entries to ch then closes it.
func sendAndClose(ch chan types.EnrichedEntry, entries []types.EnrichedEntry) {
	for _, e := range entries {
		ch <- e
	}
	close(ch)
}

// ─── Token estimation ────────────────────────────────────────────────────────

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		name  string
		input string
		// We only verify the rough range, not an exact value.
		minTok int
		maxTok int
	}{
		{"empty", "", 0, 0},
		{"single char", "a", 1, 1},
		{"seven chars ~2 tokens", "abcdefg", 1, 3},
		{"70 chars ~20 tokens", strings.Repeat("a", 70), 18, 22},
		{"350 chars ~100 tokens", strings.Repeat("x", 350), 98, 102},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateTokens(tc.input)
			assert.GreaterOrEqual(t, got, tc.minTok, "too few tokens")
			assert.LessOrEqual(t, got, tc.maxTok, "too many tokens")
		})
	}
}

// ─── Priority scoring ────────────────────────────────────────────────────────

func TestScore_LevelOrdering(t *testing.T) {
	base := time.Now()
	fatal := makeEntry(types.LevelFatal, "crash", "svc", base)
	err := makeEntry(types.LevelError, "fail", "svc", base)
	warn := makeEntry(types.LevelWarn, "warn", "svc", base)
	info := makeEntry(types.LevelInfo, "info", "svc", base)

	assert.Greater(t, Score(fatal), Score(err), "fatal > error")
	assert.Greater(t, Score(err), Score(warn), "error > warn")
	assert.Greater(t, Score(warn), Score(info), "warn > info")
	assert.Greater(t, Score(info), 0, "info > 0")
}

func TestScore_SpikeAddsPoints(t *testing.T) {
	base := time.Now()
	plain := makeEntry(types.LevelError, "fail", "svc", base)
	spiked := plain
	spiked.IsSpike = true

	assert.Greater(t, Score(spiked), Score(plain), "spike should increase score")
}

func TestScore_ChainAddsPoints(t *testing.T) {
	base := time.Now()
	plain := makeEntry(types.LevelError, "fail", "svc", base)
	chained := plain
	chained.ChainID = "db-connection-exhaustion-1"

	assert.Greater(t, Score(chained), Score(plain))
}

func TestScore_DeploymentAddsPoints(t *testing.T) {
	base := time.Now()
	plain := makeEntry(types.LevelWarn, "slow", "svc", base)
	deploy := plain
	deploy.IsDeployment = true

	assert.Greater(t, Score(deploy), Score(plain))
}

func TestScore_OccurrenceBonus(t *testing.T) {
	base := time.Now()
	once := makeEntry(types.LevelError, "fail", "svc", base)
	once.OccurrenceCount = 1

	many := makeEntry(types.LevelError, "fail", "svc", base)
	many.OccurrenceCount = 20

	assert.Greater(t, Score(many), Score(once), "higher occurrence should score higher")
}

func TestScore_OccurrenceCapped(t *testing.T) {
	base := time.Now()
	cap20 := makeEntry(types.LevelError, "fail", "svc", base)
	cap20.OccurrenceCount = occurrenceCap

	over := makeEntry(types.LevelError, "fail", "svc", base)
	over.OccurrenceCount = occurrenceCap * 10

	assert.Equal(t, Score(cap20), Score(over), "occurrence should be capped")
}

// ─── ilog2 edge cases ────────────────────────────────────────────────────────

func TestIlog2(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want int
	}{
		{"negative", -1, 0},
		{"zero", 0, 0},
		{"one", 1, 0},
		{"two", 2, 1},
		{"three", 3, 1},
		{"four", 4, 2},
		{"eight", 8, 3},
		{"1024", 1024, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ilog2(tc.n))
		})
	}
}

// ─── Run function ────────────────────────────────────────────────────────────

func TestRun_EmptyInput(t *testing.T) {
	ch := make(chan types.EnrichedEntry)
	close(ch)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)
	assert.NotEmpty(t, summary)
	assert.Contains(t, summary, "No significant log entries found")
}

func TestRun_ContextCancellation(t *testing.T) {
	ch := make(chan types.EnrichedEntry) // never closed / no sends

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Run(ctx, ch, Options{})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRun_BudgetCompliance(t *testing.T) {
	budget := 500 // small budget to force truncation

	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	var entries []types.EnrichedEntry
	for i := 0; i < 200; i++ {
		e := makeEntry(types.LevelError,
			"this is a long error message that consumes tokens",
			"service-alpha",
			base.Add(time.Duration(i)*time.Second))
		entries = append(entries, e)
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{TokenBudget: budget})
	require.NoError(t, err)

	actual := EstimateTokens(summary)
	// Allow a small over-run: up to one entry per section that bypasses the
	// budget check (first entry is always written to avoid empty sections).
	assert.LessOrEqual(t, actual, budget+50,
		"estimated tokens %d should be within budget %d (+50 slack)", actual, budget)
}

func TestRun_PriorityOrdering(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Feed: one warn first, then one fatal.  Compact must put fatal in output first.
	warn := makeEntry(types.LevelWarn, "slow response", "api", base)
	fatal := makeEntry(types.LevelFatal, "process crashed", "api", base.Add(time.Second))

	ch := make(chan types.EnrichedEntry, 2)
	sendAndClose(ch, []types.EnrichedEntry{warn, fatal})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	// The fatal message should appear before the warn message in the output.
	fatalIdx := strings.Index(summary, "process crashed")
	warnIdx := strings.Index(summary, "slow response")

	assert.NotEqual(t, -1, fatalIdx, "fatal message not found in summary")
	assert.NotEqual(t, -1, warnIdx, "warn message not found in summary")
	assert.Less(t, fatalIdx, warnIdx, "fatal entry should appear before warn entry")
}

func TestRun_SectionsPresent(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	entries := []types.EnrichedEntry{
		makeEntry(types.LevelError, "db query failed", "db", base),
		func() types.EnrichedEntry {
			e := makeEntry(types.LevelError, "connection exhausted", "db", base.Add(time.Second))
			e.ChainID = "db-connection-exhaustion-1"
			return e
		}(),
		func() types.EnrichedEntry {
			e := makeEntry(types.LevelWarn, "high request rate", "api", base.Add(2*time.Second))
			e.IsSpike = true
			return e
		}(),
		makeEntry(types.LevelWarn, "latency elevated", "api", base.Add(3*time.Second)),
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Contains(t, summary, "## Incident Overview")
	assert.Contains(t, summary, "## Critical Errors")
	assert.Contains(t, summary, "## Error Chains")
	assert.Contains(t, summary, "## Rate Anomalies")
	assert.Contains(t, summary, "## Context")
}

func TestRun_ChainDeduplication(t *testing.T) {
	// Two entries with the same chain ID should appear as one entry in Error Chains.
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	mkChain := func(msg string, offset time.Duration) types.EnrichedEntry {
		e := makeEntry(types.LevelError, msg, "db", base.Add(offset))
		e.ChainID = "db-connection-exhaustion-1"
		return e
	}

	entries := []types.EnrichedEntry{
		mkChain("lock timeout", 0),
		mkChain("connection pool exhausted", time.Second),
		mkChain("query failed", 2*time.Second),
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	// The chain ID should appear in the Error Chains section exactly once.
	chainSection := extractSection(summary, "## Error Chains", "## Rate Anomalies")
	count := strings.Count(chainSection, "db-connection-exhaustion-1")
	assert.Equal(t, 1, count, "chain ID should appear exactly once in Error Chains section")
}

func TestRun_SpikeInRateAnomalies(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	spike := makeEntry(types.LevelWarn, "error rate spike detected", "api", base)
	spike.IsSpike = true

	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{spike})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	anomalySection := extractSection(summary, "## Rate Anomalies", "## Context")
	assert.Contains(t, anomalySection, "error rate spike detected")
}

func TestRun_DeploymentFlagInOutput(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	deploy := makeEntry(types.LevelInfo, "new version deployed", "deploy", base)
	deploy.IsDeployment = true

	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{deploy})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	// The deploy annotation should appear somewhere in the summary.
	assert.Contains(t, summary, "deploy")
	assert.Contains(t, summary, "new version deployed")
}

func TestRun_LargeBudgetNoTruncation(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	entries := []types.EnrichedEntry{
		makeEntry(types.LevelError, "error one", "svc", base),
		makeEntry(types.LevelError, "error two", "svc", base.Add(time.Second)),
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{TokenBudget: 100_000})
	require.NoError(t, err)

	// All messages should be present — nothing truncated.
	assert.Contains(t, summary, "error one")
	assert.Contains(t, summary, "error two")
	assert.NotContains(t, summary, "omitted")
}

func TestRun_OccurrenceCountAnnotated(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e := makeEntry(types.LevelError, "repeated failure", "svc", base)
	e.OccurrenceCount = 42

	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{e})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Contains(t, summary, "×42")
}

// ─── Mutually exclusive sections ─────────────────────────────────────────────

func TestRun_MutuallyExclusiveSections(t *testing.T) {
	// An error entry with both ChainID and IsSpike should appear in Error Chains
	// only, not in Critical Errors or Rate Anomalies.
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e := makeEntry(types.LevelError, "chain-and-spike entry", "svc", base)
	e.ChainID = "test-chain-1"
	e.IsSpike = true

	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{e})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	chainSection := extractSection(summary, "## Error Chains", "## Rate Anomalies")
	criticalSection := extractSection(summary, "## Critical Errors", "## Error Chains")
	anomalySection := extractSection(summary, "## Rate Anomalies", "## Context")

	assert.Contains(t, chainSection, "chain-and-spike entry",
		"entry should appear in Error Chains")
	assert.NotContains(t, criticalSection, "chain-and-spike entry",
		"entry should NOT appear in Critical Errors (claimed by chains)")
	assert.NotContains(t, anomalySection, "chain-and-spike entry",
		"entry should NOT appear in Rate Anomalies (claimed by chains)")
}

// ─── Debug/Trace filtering ──────────────────────────────────────────────────

func TestRun_DebugTraceFiltered(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	entries := []types.EnrichedEntry{
		makeEntry(types.LevelDebug, "debug noise", "svc", base),
		makeEntry(types.LevelTrace, "trace noise", "svc", base.Add(time.Second)),
		makeEntry(types.LevelError, "real error", "svc", base.Add(2*time.Second)),
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.NotContains(t, summary, "debug noise")
	assert.NotContains(t, summary, "trace noise")
	assert.Contains(t, summary, "real error")
}

// ─── Overview content ────────────────────────────────────────────────────────

func TestRun_OverviewContent(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	entries := []types.EnrichedEntry{
		makeEntry(types.LevelFatal, "crash", "app-server", base),
		makeEntry(types.LevelError, "timeout", "db-proxy", base.Add(30*time.Second)),
		func() types.EnrichedEntry {
			e := makeEntry(types.LevelWarn, "spike", "api-gw", base.Add(time.Minute))
			e.IsSpike = true
			e.ChainID = "cascade-1"
			return e
		}(),
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	overview := extractSection(summary, "## Incident Overview", "## Critical Errors")

	// Time window
	assert.Contains(t, overview, "2025-01-01T12:00:00Z", "should contain start time")
	assert.Contains(t, overview, "2025-01-01T12:01:00Z", "should contain end time")
	assert.Contains(t, overview, "1m0s", "should contain duration")

	// Source count
	assert.Contains(t, overview, "Sources: 3", "should report 3 sources")

	// Event counts
	assert.Contains(t, overview, "1 fatal", "should report fatal count")
	assert.Contains(t, overview, "1 error", "should report error count")
	assert.Contains(t, overview, "1 warn", "should report warn count")

	// Chain detection
	assert.Contains(t, overview, "cascade-1", "should report chain ID")

	// Spike count
	assert.Contains(t, overview, "Rate spikes: 1", "should report spike count")
}

// ─── truncateToTokens ────────────────────────────────────────────────────────

func TestTruncateToTokens(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		budget int
		check  func(t *testing.T, result string)
	}{
		{
			name:   "within budget unchanged",
			input:  "short",
			budget: 100,
			check: func(t *testing.T, result string) {
				assert.Equal(t, "short", result)
			},
		},
		{
			name:   "empty string",
			input:  "",
			budget: 10,
			check: func(t *testing.T, result string) {
				assert.Equal(t, "", result)
			},
		},
		{
			name:   "exactly at budget",
			input:  strings.Repeat("a", 35), // ~10 tokens
			budget: 10,
			check: func(t *testing.T, result string) {
				assert.Equal(t, strings.Repeat("a", 35), result)
			},
		},
		{
			name:   "over budget snaps to newline",
			input:  "line one\nline two\nline three\nline four\nline five",
			budget: 5, // very tight
			check: func(t *testing.T, result string) {
				assert.Contains(t, result, "... (truncated)")
				assert.LessOrEqual(t, len(result), len("line one\nline two\nline three\nline four\nline five"))
			},
		},
		{
			name:   "over budget no newlines",
			input:  strings.Repeat("x", 200),
			budget: 5,
			check: func(t *testing.T, result string) {
				assert.Contains(t, result, "... (truncated)")
			},
		},
		{
			name:   "zero budget",
			input:  "some text",
			budget: 0,
			check: func(t *testing.T, result string) {
				// With zero budget, should still truncate.
				assert.Contains(t, result, "... (truncated)")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateToTokens(tc.input, tc.budget)
			tc.check(t, result)
		})
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// extractSection returns the text between the line starting with startHeader
// and the line starting with endHeader (exclusive).
func extractSection(text, startHeader, endHeader string) string {
	start := strings.Index(text, startHeader)
	if start == -1 {
		return ""
	}
	start += len(startHeader)
	end := strings.Index(text[start:], endHeader)
	if end == -1 {
		return text[start:]
	}
	return text[start : start+end]
}

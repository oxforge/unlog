package compact

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func chainMember(level types.Level, msg string, first, last time.Time, count int, id string, step, steps int) types.EnrichedEntry {
	e := spanEntry(level, msg, "app.log", first, last, count)
	e.ChainID, e.ChainStep, e.ChainSteps = id, step, steps
	return e
}

func TestRun_ChainShowsEveryMemberInOrder(t *testing.T) {
	at := func(m, s int) time.Time { return time.Date(2024, 1, 15, 10, m, s, 0, time.UTC) }
	entries := []types.EnrichedEntry{
		chainMember(types.LevelError, "circuit breaker opened: database", at(31, 20), at(31, 20), 1, "circuit-breaker", 2, 3),
		chainMember(types.LevelError, "database connection timeout: context deadline exceeded", at(31, 10), at(31, 13), 4, "circuit-breaker", 1, 3),
		spanEntry(types.LevelError, "payment declined", "app.log", at(31, 15), at(31, 15), 1),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Equal(t, "\n"+
		"circuit-breaker: 2 of 3 steps, 2024-01-15T10:31:10Z–10:31:20Z\n"+
		"  1. 2024-01-15T10:31:10Z–10:31:13Z [ERROR] app.log: database connection timeout: context deadline exceeded {×4}\n"+
		"  2. 2024-01-15T10:31:20Z [ERROR] app.log: circuit breaker opened: database\n\n",
		extractSection(summary, "## Error Chains", "## Rate Anomalies"))
	critical := extractSection(summary, "## Critical Errors", "## Error Chains")
	assert.Contains(t, critical, "payment declined")
	assert.NotContains(t, critical, "circuit breaker opened")
}

func TestRun_ChainMembersBelowErrorStayInTheChain(t *testing.T) {
	at := time.Date(2024, 1, 15, 14, 1, 55, 0, time.UTC)
	entries := []types.EnrichedEntry{
		chainMember(types.LevelInfo, "deployment starting for api-server v2.4.1", at, at, 1, "deployment-failure", 1, 3),
		chainMember(types.LevelWarn, "readiness probe failed for api-server", at.Add(time.Minute), at.Add(time.Minute), 1, "deployment-failure", 2, 3),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	chains := extractSection(summary, "## Error Chains", "## Rate Anomalies")
	assert.Contains(t, chains, "deployment-failure: 2 of 3 steps")
	assert.Contains(t, chains, "  1. 2024-01-15T14:01:55Z [INFO] app.log: deployment starting")
	assert.Contains(t, chains, "  2. 2024-01-15T14:02:55Z [WARN] app.log: readiness probe failed")
	assert.Equal(t, "\n(none)", extractSection(summary, "## Context", "\x00"))
	assert.NotContains(t, summary, "chain=")
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

// ─── Levels below warn ──────────────────────────────────────────────────────

// The filter stage applies --level; compact must not drop what it let through,
// and entries whose level is unknown are kept by the filter on purpose.
func TestRun_KeepsLowAndUnknownLevelsInContext(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	entries := []types.EnrichedEntry{
		makeEntry(types.LevelUnknown, "LOG:  checkpoint starting", "db.log", base),
		makeEntry(types.LevelDebug, "cache lookup for key 42", "svc", base.Add(time.Second)),
		makeEntry(types.LevelTrace, "entering handler", "svc", base.Add(2*time.Second)),
		makeEntry(types.LevelError, "real error", "svc", base.Add(3*time.Second)),
	}

	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	contextSection := extractSection(summary, "## Context", "\x00")
	assert.Contains(t, contextSection, "LOG:  checkpoint starting")
	assert.Contains(t, contextSection, "cache lookup for key 42")
	assert.Contains(t, contextSection, "entering handler")
	assert.Contains(t, extractSection(summary, "## Critical Errors", "## Error Chains"), "real error")
}

func TestRun_UnknownLevelRanksBelowWarn(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// The unknown entry is the earliest, so only its score can keep it out.
	entries := []types.EnrichedEntry{makeEntry(types.LevelUnknown, "unclassified line", "db.log", base)}
	for i := 0; i < 30; i++ {
		entries = append(entries, makeEntry(types.LevelWarn,
			fmt.Sprintf("queue depth rising on shard %02d", i), "svc", base.Add(time.Duration(i+1)*time.Second)))
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	// ~300 tokens fit about 14 of the 31 lines.
	summary, err := Run(context.Background(), ch, Options{TokenBudget: 300})
	require.NoError(t, err)

	contextSection := extractSection(summary, "## Context", "\x00")
	assert.Contains(t, contextSection, "queue depth rising")
	assert.Contains(t, contextSection, "more entries omitted")
	assert.NotContains(t, contextSection, "unclassified line")
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

	// Spike-flagged entries are not bursts; the overview counts bursts only.
	assert.NotContains(t, overview, "Rate spikes")
	assert.NotContains(t, overview, "Bursts:")
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

func TestRun_LongMessageIsCutInTheSummary(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	long := "payload dump: " + strings.Repeat("é", 3000) + " TAIL-MARKER"

	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{makeEntry(types.LevelError, long, "svc", base)})

	summary, err := Run(context.Background(), ch, Options{TokenBudget: 1_000_000})
	require.NoError(t, err)

	assert.Contains(t, summary, "payload dump: ééé")
	assert.NotContains(t, summary, "TAIL-MARKER")
	assert.Contains(t, summary, "more bytes]")
	for _, line := range strings.Split(summary, "\n") {
		assert.LessOrEqual(t, len(line), 2200, "a summary line should be capped near 2000 bytes")
		assert.True(t, utf8.ValidString(line), "the cut must not split a UTF-8 character")
	}
}

// ─── Entries without timestamps (M1) ────────────────────────────────────────

func TestRun_OverviewCountsEntriesWithoutTimestamps(t *testing.T) {
	ts := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	entries := []types.EnrichedEntry{
		makeEntry(types.LevelError, "max_connections reached", "db.log", time.Time{}),
		makeEntry(types.LevelError, "request failed", "app.log", ts),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	overview := extractSection(summary, "## Incident Overview", "## Critical Errors")
	assert.Contains(t, overview, "Window: 2025-01-01T12:00:00Z — 2025-01-01T12:00:00Z (0s)")
	assert.Contains(t, overview, "Sources: 2 (app.log, db.log)")
	assert.Contains(t, overview, "Events: 0 fatal, 2 error, 0 warn")
}

func TestRun_EntryWithoutTimestampShowsNoDate(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeEntry(types.LevelError, "max_connections reached", "db.log", time.Time{}),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.NotContains(t, summary, "0001-01-01")
	assert.Contains(t, summary, "Window: unknown (no timestamps)")
	assert.Contains(t, summary, "\n- [ERROR] db.log: max_connections reached")
}

// ─── Budget and order within sections (M4) ──────────────────────────────────

func TestRun_UnusedSectionBudgetGoesToOtherSections(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	var entries []types.EnrichedEntry
	for i := 0; i < 60; i++ {
		entries = append(entries, makeEntry(types.LevelError,
			fmt.Sprintf("payment %02d declined by issuer", i), "svc", base.Add(time.Duration(i)*time.Second)))
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{TokenBudget: 1000})
	require.NoError(t, err)

	// Each line is ~60 bytes (~18 tokens). The Critical share alone (45%,
	// ~445 tokens) fits about 24; with the empty sections' shares it fits
	// more than 40.
	critical := extractSection(summary, "## Critical Errors", "## Error Chains")
	assert.Greater(t, strings.Count(critical, "declined by issuer"), 40)
	assert.LessOrEqual(t, EstimateTokens(summary), 1000)
}

func TestRun_SectionListsEntriesInTimeOrder(t *testing.T) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	entries := []types.EnrichedEntry{
		makeEntry(types.LevelFatal, "third: process crashed", "svc", base.Add(3*time.Second)),
		makeEntry(types.LevelError, "first: disk slow", "svc", base.Add(1*time.Second)),
		makeEntry(types.LevelError, "undated: worker gone", "svc", time.Time{}),
		makeEntry(types.LevelError, "second: write failed", "svc", base.Add(2*time.Second)),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	critical := extractSection(summary, "## Critical Errors", "## Error Chains")
	first := strings.Index(critical, "first:")
	second := strings.Index(critical, "second:")
	third := strings.Index(critical, "third:")
	undated := strings.Index(critical, "undated:")
	assert.True(t, first >= 0 && first < second && second < third && third < undated,
		"want time order with undated last, got:\n%s", critical)
}

// ─── Error spans and occurrence counts ──────────────────────────────────────

func spanEntry(level types.Level, msg, source string, first, last time.Time, count int) types.EnrichedEntry {
	e := makeEntry(level, msg, source, first)
	e.FirstSeen = first
	e.LastSeen = last
	e.OccurrenceCount = count
	return e
}

func TestFormatEntrySpan(t *testing.T) {
	day := func(d, h, m, s int) time.Time { return time.Date(2024, 1, d, h, m, s, 0, time.UTC) }
	tests := []struct {
		name  string
		entry types.EnrichedEntry
		want  string
	}{
		{"same day", spanEntry(types.LevelError, "cache miss", "api.log", day(15, 10, 0, 0), day(15, 10, 9, 50), 60),
			"2024-01-15T10:00:00Z–10:09:50Z [ERROR] api.log: cache miss {×60}"},
		{"across midnight", spanEntry(types.LevelError, "cache miss", "api.log", day(15, 23, 59, 0), day(16, 0, 1, 0), 2),
			"2024-01-15T23:59:00Z–2024-01-16T00:01:00Z [ERROR] api.log: cache miss {×2}"},
		{"single moment", spanEntry(types.LevelError, "cache miss", "api.log", day(15, 10, 0, 0), day(15, 10, 0, 0), 1),
			"2024-01-15T10:00:00Z [ERROR] api.log: cache miss"},
		{"no last seen", spanEntry(types.LevelError, "cache miss", "api.log", day(15, 10, 0, 0), time.Time{}, 1),
			"2024-01-15T10:00:00Z [ERROR] api.log: cache miss"},
		{"undated", spanEntry(types.LevelError, "cache miss", "api.log", time.Time{}, time.Time{}, 3),
			"- [ERROR] api.log: cache miss {×3}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatEntry(tt.entry))
		})
	}
}

func TestRun_OverviewSpansAndOccurrences(t *testing.T) {
	first := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	last := time.Date(2024, 1, 15, 10, 49, 59, 0, time.UTC)
	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{spanEntry(types.LevelError, "payment declined for order 0", "app.log", first, last, 3000)})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	overview := extractSection(summary, "## Incident Overview", "## Critical Errors")
	assert.Contains(t, overview, "Window: 2024-01-15T10:00:00Z — 2024-01-15T10:49:59Z (49m59s)")
	assert.Contains(t, overview, "Events: 0 fatal, 3000 error (1 distinct), 0 warn")
}

func TestRun_OverviewDistinctOnlyWhenRepeated(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := []types.EnrichedEntry{
		spanEntry(types.LevelError, "payment declined", "app.log", at, at.Add(time.Hour), 2900),
		spanEntry(types.LevelError, "refund failed", "app.log", at, at.Add(time.Minute), 100),
		spanEntry(types.LevelFatal, "ledger corrupted", "app.log", at, at, 1),
	}
	for i := 0; i < 7; i++ {
		entries = append(entries, spanEntry(types.LevelWarn, fmt.Sprintf("queue %d slow", i), "app.log", at, at, 1))
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Contains(t, summary, "Events: 1 fatal, 3000 error (2 distinct), 7 warn\n")
}

// ─── Bursts ─────────────────────────────────────────────────────────────────

func exampleBurst() types.Burst {
	return types.Burst{
		Source: "same.log",
		Start:  time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
		End:    time.Date(2024, 1, 15, 10, 5, 9, 0, time.UTC),
		Events: 401,
		TopErrors: []types.BurstError{
			{Message: "payment gateway refused connection from 10.0.3.0", Level: types.LevelError, Count: 400},
			{Message: "cache miss for key user:30", Level: types.LevelError, Count: 1},
		},
	}
}

func runWithBursts(t *testing.T, entries []types.EnrichedEntry, bursts []types.Burst) string {
	t.Helper()
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)
	summary, err := Run(context.Background(), ch, Options{Bursts: func() []types.Burst { return bursts }})
	require.NoError(t, err)
	return summary
}

func TestRun_RateAnomaliesListsBursts(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)
	summary := runWithBursts(t,
		[]types.EnrichedEntry{makeEntry(types.LevelError, "payment gateway refused connection from 10.0.3.0", "same.log", at)},
		[]types.Burst{exampleBurst()})

	assert.Equal(t, "\n"+
		"2024-01-15T10:05:00Z–10:05:09Z same.log: 401 events in 10s (normally quiet)\n"+
		"  payment gateway refused connection from 10.0.3.0 {×400}\n"+
		"  cache miss for key user:30\n\n",
		extractSection(summary, "## Rate Anomalies", "## Context"))
}

func TestRun_BurstWithBaselineShowsRatio(t *testing.T) {
	b := exampleBurst()
	b.Events = 300
	b.Baseline = 2.0
	summary := runWithBursts(t,
		[]types.EnrichedEntry{makeEntry(types.LevelError, "x", "same.log", b.Start)}, []types.Burst{b})

	assert.Contains(t, summary, "same.log: 300 events in 10s (normal ~2.0/s, 15× above)\n")
}

func TestRun_SpikeEntriesStayInCriticalErrors(t *testing.T) {
	e := makeEntry(types.LevelError, "payment gateway refused connection", "same.log", time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC))
	e.IsSpike = true
	summary := runWithBursts(t, []types.EnrichedEntry{e}, nil)

	assert.Contains(t, extractSection(summary, "## Critical Errors", "## Error Chains"), "payment gateway refused connection {spike}")
	assert.Equal(t, "\n(none)\n\n", extractSection(summary, "## Rate Anomalies", "## Context"))
}

func TestRun_OverviewCountsBursts(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)
	entries := []types.EnrichedEntry{makeEntry(types.LevelError, "x", "same.log", at)}
	second := exampleBurst()
	second.Source = "other.log"

	withBursts := runWithBursts(t, entries, []types.Burst{exampleBurst(), second})
	assert.Contains(t, extractSection(withBursts, "## Incident Overview", "## Critical Errors"), "Bursts: 2")

	without := runWithBursts(t, entries, nil)
	assert.NotContains(t, without, "Bursts:")
}

// On cancellation an upstream stage can close compact's input before the
// filter has finished writing its bursts, so a cancelled run must not read
// them.
func TestRun_CancelledRunDoesNotReadBursts(t *testing.T) {
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ch := make(chan types.EnrichedEntry)
		close(ch)
		read := false

		_, err := Run(ctx, ch, Options{Bursts: func() []types.Burst { read = true; return nil }})

		require.ErrorIs(t, err, context.Canceled)
		require.False(t, read, "Bursts was read on a cancelled run")
	}
}

func TestRun_OverviewCountsEachOccurrenceAtItsOwnLevel(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	e := spanEntry(types.LevelFatal, "ledger write failed", "app.log", at, at.Add(time.Hour), 69728)
	e.OccurrencesByLevel = map[types.Level]int{types.LevelError: 48876, types.LevelFatal: 20852}
	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{e})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Contains(t, summary, "Events: 20852 fatal (1 distinct), 48876 error (1 distinct), 0 warn\n")
}

// ─── Deployments ────────────────────────────────────────────────────────────

func deployEntry(level types.Level, msg string, at time.Time) types.EnrichedEntry {
	e := spanEntry(level, msg, "deploy.log", at, at, 1)
	e.IsDeployment = true
	return e
}

func TestRun_DeploymentsComeBeforeTheErrors(t *testing.T) {
	at := func(m, s int) time.Time { return time.Date(2024, 1, 15, 14, m, s, 0, time.UTC) }
	entries := []types.EnrichedEntry{
		spanEntry(types.LevelError, "health check failed", "app.log", at(2, 15), at(2, 15), 1),
		deployEntry(types.LevelInfo, "rolling update initiated: 3 pods", at(2, 0)),
		deployEntry(types.LevelInfo, "deployment started: app to 3.3.0", at(1, 55)),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Equal(t, "\n"+
		"2024-01-15T14:01:55Z [INFO] deploy.log: deployment started: app to 3.3.0 {deploy}\n"+
		"2024-01-15T14:02:00Z [INFO] deploy.log: rolling update initiated: 3 pods {deploy}\n\n",
		extractSection(summary, "## Deployments", "## Critical Errors"))
	assert.Less(t, strings.Index(summary, "## Deployments"), strings.Index(summary, "## Critical Errors"))
	assert.Contains(t, extractSection(summary, "## Incident Overview", "## Deployments"),
		"Last deployment: 2024-01-15T14:02:00Z, 15s before the first error")
	assert.NotContains(t, extractSection(summary, "## Context", "\x00"), "deploy.log")
}

func TestRun_NoDeploymentBeforeTheFirstError(t *testing.T) {
	at := time.Date(2024, 1, 15, 14, 2, 0, 0, time.UTC)
	entries := []types.EnrichedEntry{
		spanEntry(types.LevelError, "health check failed", "app.log", at, at, 1),
		deployEntry(types.LevelInfo, "rolling update initiated", at.Add(time.Minute)),
	}
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.NotContains(t, summary, "Last deployment")
	assert.Contains(t, extractSection(summary, "## Deployments", "## Critical Errors"), "rolling update initiated")
}

func TestRun_ErrorLevelDeployLineStaysInCriticalErrors(t *testing.T) {
	at := time.Date(2024, 1, 15, 14, 2, 0, 0, time.UTC)
	ch := make(chan types.EnrichedEntry, 1)
	sendAndClose(ch, []types.EnrichedEntry{deployEntry(types.LevelError, "service restarted after crash", at)})

	summary, err := Run(context.Background(), ch, Options{})
	require.NoError(t, err)

	assert.Contains(t, extractSection(summary, "## Critical Errors", "## Error Chains"), "service restarted after crash {deploy}")
	assert.Contains(t, extractSection(summary, "## Deployments", "## Critical Errors"), "service restarted after crash")
}

// ─── Services and concurrent failures ───────────────────────────────────────

func TestFormatEntryShowsTheService(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	e := spanEntry(types.LevelError, "card declined", "agg.log", at, at, 1)
	e.Service = "payments"

	assert.Equal(t, "2024-01-15T10:00:00Z [ERROR] agg.log[payments]: card declined", formatEntry(e))
}

func TestFormatEntryNamesConcurrentFailures(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	e := spanEntry(types.LevelError, "request failed", "api.log", at, at, 1)
	e.CorrelatedWith = []string{"agg.log[payments]", "cache.log", "db.log"}
	e.CorrelatedCount = 7

	assert.Equal(t, "2024-01-15T10:00:00Z [ERROR] api.log: request failed {with=agg.log[payments],cache.log,+5}", formatEntry(e))
}

func TestScore_ConcurrentFailureAddsPoints(t *testing.T) {
	alone := makeEntry(types.LevelError, "x", "api.log", time.Time{})
	together := alone
	together.CorrelatedWith = []string{"db.log"}

	assert.Greater(t, Score(together), Score(alone))
}

func TestRun_BurstHeaderShowsTheService(t *testing.T) {
	b := exampleBurst()
	b.Source, b.Service = "agg.log", "payments"
	summary := runWithBursts(t,
		[]types.EnrichedEntry{makeEntry(types.LevelError, "x", "agg.log", b.Start)}, []types.Burst{b})

	assert.Contains(t, summary, "2024-01-15T10:05:00Z–10:05:09Z agg.log[payments]: 401 events in 10s")
}

// chainOf builds a chain's members: n step-1 errors (from n sources when
// sameError, else n different errors from one source) and one step-2 line.
func chainOf(id string, at time.Time, n int, sameError bool) []types.EnrichedEntry {
	var entries []types.EnrichedEntry
	for i := range n {
		msg, source := "timeout connecting to payment-api", fmt.Sprintf("svc-%03d.log", i)
		if !sameError {
			msg, source = fmt.Sprintf("timeout connecting to shard %03d of the ledger cluster", i), "app.log"
		}
		e := chainMember(types.LevelError, msg, at.Add(time.Duration(i)*100*time.Millisecond),
			at.Add(time.Duration(i)*100*time.Millisecond), 1, id, 1, 3)
		e.Source, e.Signature = source, msg
		entries = append(entries, e)
	}
	breaker := chainMember(types.LevelWarn, "circuit breaker open for payment-api", at.Add(time.Minute), at.Add(time.Minute), 1, id, 2, 3)
	breaker.Signature = breaker.Message
	return append(entries, breaker)
}

func runEntries(t *testing.T, entries []types.EnrichedEntry, opts Options) string {
	t.Helper()
	ch := make(chan types.EnrichedEntry, len(entries))
	sendAndClose(ch, entries)
	summary, err := Run(context.Background(), ch, opts)
	require.NoError(t, err)
	return summary
}

// The same error at one step, from many sources, is one line.
func TestRun_ChainCollapsesTheSameErrorAcrossSources(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	chains := sectionOf(runEntries(t, chainOf("circuit-breaker", at, 400, true), Options{}), "## Error Chains")

	assert.Equal(t, 1, strings.Count(chains, "timeout connecting to payment-api"), chains)
	assert.Contains(t, chains, "1. 2024-01-15T10:00:00Z–10:00:39Z [ERROR] svc-000.log: timeout connecting to payment-api {×400} (also from 399 other sources)")
	assert.Contains(t, chains, "2. 2024-01-15T10:01:00Z [WARN]")
}

// Many different errors at one step show the first few, then a count, so
// the later steps stay visible.
func TestRun_ChainCapsTheLinesOfAStep(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	chains := sectionOf(runEntries(t, chainOf("circuit-breaker", at, 300, false), Options{}), "## Error Chains")

	assert.Equal(t, 3, strings.Count(chains, "  1. "), chains)
	assert.Contains(t, chains, "  … and 297 more at step 1")
	assert.Contains(t, chains, "  2. 2024-01-15T10:01:00Z [WARN]")
}

// Chains stay within the budget, and one that doesn't fit still shows its
// header instead of vanishing.
func TestRun_ChainsStayWithinTheBudget(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := chainOf("circuit-breaker", at, 300, false)
	for i := range 20 {
		id := fmt.Sprintf("circuit-breaker-%d", i+2)
		entries = append(entries, chainOf(id, at.Add(time.Duration(i+1)*time.Hour), 30, false)...)
	}

	summary := runEntries(t, entries, Options{TokenBudget: 2000})

	assert.LessOrEqual(t, EstimateTokens(summary), 2000+50)
	chains := sectionOf(summary, "## Error Chains")
	assert.Contains(t, chains, "circuit-breaker: 2 of 3 steps")
	assert.Contains(t, chains, "circuit-breaker-21: 2 of 3 steps", "a chain without room for its entries keeps its header")
}

// The best item of a section alone may be over its budget; it is cut to the
// lines that fit, its first line kept.
func TestRun_AnItemOverTheBudgetIsCut(t *testing.T) {
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	entries := chainOf("circuit-breaker", at, 3, false)
	for i := range entries[:3] {
		entries[i].Message += " " + strings.Repeat("detail ", 200)
		entries[i].Signature = entries[i].Message
	}

	summary := runEntries(t, entries, Options{TokenBudget: 600})

	chains := sectionOf(summary, "## Error Chains")
	assert.Contains(t, chains, "circuit-breaker: 2 of 3 steps")
	assert.Contains(t, chains, "more lines")
	assert.LessOrEqual(t, EstimateTokens(summary), 600+50)
}

// sectionOf returns the body of the summary section that starts at header.
func sectionOf(summary, header string) string {
	start := strings.Index(summary, header)
	if start < 0 {
		return ""
	}
	body := summary[start+len(header):]
	if end := strings.Index(body, "\n## "); end >= 0 {
		body = body[:end]
	}
	return body
}

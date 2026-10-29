package enrich

import (
	"fmt"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var chainBase = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

func makeChainEntry(msg string, level types.Level, source string, ts time.Time, lineNum int64) types.EnrichedEntry {
	return types.EnrichedEntry{
		FilteredEntry: types.FilteredEntry{
			LogEntry: types.LogEntry{
				Timestamp:  ts,
				Level:      level,
				Source:     source,
				Message:    msg,
				LineNumber: lineNum,
			},
		},
	}
}

// step is a chain member as (entry index, step number), for compact asserts.
type step struct{ index, step int }

func membersOf(c Chain) []step {
	out := make([]step, len(c.Members))
	for i, m := range c.Members {
		out[i] = step{m.Index, m.Step}
	}
	return out
}

func TestFindChains_EveryBuiltinPatternCompletes(t *testing.T) {
	tests := []struct {
		pattern string
		stages  [3]types.EnrichedEntry
	}{
		{"db-connection-exhaustion", [3]types.EnrichedEntry{
			makeChainEntry("deadlock detected on table users", types.LevelWarn, "db", chainBase, 1),
			makeChainEntry("connection pool exhausted, 0 available", types.LevelError, "db", chainBase.Add(30*time.Second), 2),
			makeChainEntry("query failed: cannot execute SELECT", types.LevelError, "app", chainBase.Add(60*time.Second), 3)}},
		{"oom-cascade", [3]types.EnrichedEntry{
			makeChainEntry("memory warning: heap usage exceeds 90%", types.LevelWarn, "app", chainBase, 1),
			makeChainEntry("OOM killed process 1234", types.LevelError, "kernel", chainBase.Add(time.Minute), 2),
			makeChainEntry("pod restart: app-server-xyz", types.LevelError, "k8s", chainBase.Add(90*time.Second), 3)}},
		{"deployment-failure", [3]types.EnrichedEntry{
			makeChainEntry("deployment starting for api-server v2.4.1, pulling image", types.LevelInfo, "deploy", chainBase, 1),
			makeChainEntry("health check failed for api-server: HTTP 503", types.LevelWarn, "k8s", chainBase.Add(3*time.Minute), 2),
			makeChainEntry("rollback initiated: reverting api-server to v2.3.9", types.LevelError, "deploy", chainBase.Add(5*time.Minute), 3)}},
		{"circuit-breaker", [3]types.EnrichedEntry{
			makeChainEntry("timeout connecting to payment-api after 30s", types.LevelError, "gateway", chainBase, 1),
			makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "gateway", chainBase.Add(20*time.Second), 2),
			makeChainEntry("fallback activated: returning cached response", types.LevelWarn, "gateway", chainBase.Add(40*time.Second), 3)}},
		{"disk-full", [3]types.EnrichedEntry{
			makeChainEntry("disk space warning: /data partition at 95%", types.LevelWarn, "monitor", chainBase, 1),
			makeChainEntry("write failed: no space left on device", types.LevelError, "db", chainBase.Add(2*time.Minute), 2),
			makeChainEntry("fatal: service terminated due to unrecoverable error", types.LevelError, "db", chainBase.Add(3*time.Minute), 3)}},
		{"certificate-expiry", [3]types.EnrichedEntry{
			makeChainEntry("certificate expiring in 2 days for api.example.com", types.LevelWarn, "cert-manager", chainBase, 1),
			makeChainEntry("TLS handshake failed: certificate expired", types.LevelError, "nginx", chainBase.Add(2*time.Minute), 2),
			makeChainEntry("connection refused from downstream client 10.0.1.5", types.LevelError, "nginx", chainBase.Add(3*time.Minute), 3)}},
		{"dns-failure", [3]types.EnrichedEntry{
			makeChainEntry("DNS lookup failed for payments.internal.svc", types.LevelError, "resolver", chainBase, 1),
			makeChainEntry("connection failed: dial tcp: lookup payments.internal.svc: no such host", types.LevelError, "app", chainBase.Add(30*time.Second), 2),
			makeChainEntry("upstream unavailable: payments service not reachable", types.LevelError, "gateway", chainBase.Add(60*time.Second), 3)}},
		{"rate-limiting", [3]types.EnrichedEntry{
			makeChainEntry("rate limit warning: API calls approaching threshold", types.LevelWarn, "api", chainBase, 1),
			makeChainEntry("HTTP 429 too many requests from client 10.0.0.5", types.LevelError, "api", chainBase.Add(time.Minute), 2),
			makeChainEntry("client error: multiple requests failed due to throttling", types.LevelError, "client", chainBase.Add(2*time.Minute), 3)}},
		{"queue-backlog", [3]types.EnrichedEntry{
			makeChainEntry("consumer lag increasing: orders-queue 50000 messages behind", types.LevelWarn, "kafka", chainBase, 1),
			makeChainEntry("queue full: orders-queue cannot accept new messages, message rejected", types.LevelError, "kafka", chainBase.Add(5*time.Minute), 2),
			makeChainEntry("producer blocked: send timeout after 30s waiting for capacity", types.LevelError, "order-service", chainBase.Add(7*time.Minute), 3)}},
		{"cascade-failure", [3]types.EnrichedEntry{
			makeChainEntry("error: failed to process request in auth service", types.LevelError, "auth", chainBase, 1),
			makeChainEntry("request timed out waiting for upstream auth response", types.LevelError, "gateway", chainBase.Add(time.Minute), 2),
			makeChainEntry("HTTP 502 error returned to client for /api/checkout", types.LevelError, "gateway", chainBase.Add(2*time.Minute), 3)}},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			chains := NewChainMatcher().FindChains(tt.stages[:])

			var found *Chain
			for i := range chains {
				if chains[i].Pattern == tt.pattern {
					found = &chains[i]
				}
			}
			require.NotNil(t, found, "chains found: %+v", chains)
			assert.Equal(t, tt.pattern, found.ID)
			assert.Equal(t, 3, found.Steps)
			assert.Equal(t, 3, found.Matched)
			assert.Equal(t, []step{{0, 1}, {1, 2}, {2, 3}}, membersOf(*found))
		})
	}
}

func TestFindChains_OneStepIsNotAChain(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("database connection timeout: context deadline exceeded", types.LevelError, "app", chainBase, 1),
		makeChainEntry("request timeout: upstream unavailable", types.LevelError, "app", chainBase.Add(time.Second), 2),
	}

	assert.Empty(t, NewChainMatcher().FindChains(entries))
}

func TestFindChains_TwoStepsAreAChain(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("database connection timeout: context deadline exceeded", types.LevelError, "app", chainBase, 1),
		makeChainEntry("payment declined", types.LevelError, "app", chainBase.Add(5*time.Second), 2),
		makeChainEntry("circuit breaker opened: database", types.LevelError, "app", chainBase.Add(10*time.Second), 3),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 1)
	assert.Equal(t, "circuit-breaker", chains[0].ID)
	assert.Equal(t, 2, chains[0].Matched)
	assert.Equal(t, []step{{0, 1}, {2, 2}}, membersOf(chains[0]))
}

// Several errors at one step (timeouts from two sources) join the same chain
// instead of each opening its own.
func TestFindChains_RepeatedStepJoinsTheOpenChain(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("timeout connecting to payment-api", types.LevelError, "gateway", chainBase, 1),
		makeChainEntry("timeout reading from ledger", types.LevelError, "ledger", chainBase.Add(2*time.Second), 2),
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "gateway", chainBase.Add(5*time.Second), 3),
		makeChainEntry("timeout connecting to payment-api again", types.LevelError, "gateway", chainBase.Add(8*time.Second), 4),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 1)
	assert.Equal(t, []step{{0, 1}, {1, 1}, {2, 2}, {3, 1}}, membersOf(chains[0]))
}

func TestFindChains_StepOutsideTheWindowStartsOver(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("timeout connecting to payment-api", types.LevelError, "gateway", chainBase, 1),
		// circuit-breaker's window is 2 minutes.
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "gateway", chainBase.Add(3*time.Minute), 2),
	}

	assert.Empty(t, NewChainMatcher().FindChains(entries))
}

func TestFindChains_SecondChainOfAPatternIsNumbered(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("timeout connecting to payment-api", types.LevelError, "gateway", chainBase, 1),
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "gateway", chainBase.Add(10*time.Second), 2),
		makeChainEntry("timeout connecting to ledger", types.LevelError, "gateway", chainBase.Add(time.Hour), 3),
		makeChainEntry("circuit breaker open for ledger", types.LevelWarn, "gateway", chainBase.Add(time.Hour+10*time.Second), 4),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 2)
	assert.Equal(t, "circuit-breaker", chains[0].ID)
	assert.Equal(t, "circuit-breaker-2", chains[1].ID)
	assert.Equal(t, []step{{2, 1}, {3, 2}}, membersOf(chains[1]))
}

func TestFindChains_LevelBelowStageMinimumDoesNotMatch(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("deadlock detected", types.LevelInfo, "db", chainBase, 1), // stage 1 needs WARN
		makeChainEntry("connection pool exhausted", types.LevelError, "db", chainBase.Add(30*time.Second), 2),
	}

	assert.Empty(t, NewChainMatcher().FindChains(entries))
}

func TestFindChains_UndatedEntriesAreSkipped(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("timeout connecting to payment-api", types.LevelError, "gateway", time.Time{}, 1),
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "gateway", time.Time{}, 2),
	}

	assert.Empty(t, NewChainMatcher().FindChains(entries))
}

// Any step can open a chain and any later one advances it, so a log that
// skips a step, or starts after the first one, still shows the chain.
func TestFindChains_StepsMayBeSkipped(t *testing.T) {
	tests := []struct {
		name    string
		entries []types.EnrichedEntry
		pattern string
		want    []step
	}{
		{"steps 1 and 3", []types.EnrichedEntry{
			makeChainEntry("deployment started: api to v2.4.1", types.LevelInfo, "deploy", chainBase, 1),
			makeChainEntry("deployment failed: rolling back api to v2.4.0", types.LevelError, "deploy", chainBase.Add(3*time.Minute), 2),
		}, "deployment-failure", []step{{0, 1}, {1, 3}}},
		{"steps 2 and 3", []types.EnrichedEntry{
			makeChainEntry("too many connections", types.LevelError, "db", chainBase, 1),
			makeChainEntry("query failed: cannot execute SELECT", types.LevelError, "app", chainBase.Add(5*time.Second), 2),
		}, "db-connection-exhaustion", []step{{0, 2}, {1, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chains := NewChainMatcher().FindChains(tt.entries)
			require.Len(t, chains, 1, "%+v", chains)
			assert.Equal(t, tt.pattern, chains[0].Pattern)
			assert.Equal(t, 2, chains[0].Matched, "two distinct steps")
			assert.Equal(t, tt.want, membersOf(chains[0]))
		})
	}
}

// Steps must come in order: a later step logged in the same second counts,
// whatever order the files sort in; one logged after the step it should
// precede does not.
func TestFindChains_StepOrder(t *testing.T) {
	breaker := makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "api.log", chainBase, 1)

	same := NewChainMatcher().FindChains([]types.EnrichedEntry{
		breaker, makeChainEntry("timeout connecting to payment-api", types.LevelError, "worker.log", chainBase, 1)})
	require.Len(t, same, 1)
	assert.Equal(t, []step{{0, 2}, {1, 1}}, membersOf(same[0]))

	after := NewChainMatcher().FindChains([]types.EnrichedEntry{
		breaker, makeChainEntry("timeout connecting to payment-api", types.LevelError, "worker.log", chainBase.Add(30*time.Second), 1)})
	assert.Empty(t, after)
}

// Before a chain forms, an unrelated earlier match outside the window must
// not hide it.
func TestFindChains_AnEarlierMatchDoesNotHideAChain(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("timeout connecting to ledger", types.LevelError, "app", chainBase, 1),
		makeChainEntry("connection refused by payment-api", types.LevelError, "app", chainBase.Add(110*time.Second), 2),
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "app", chainBase.Add(130*time.Second), 3),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 1)
	assert.Equal(t, []step{{1, 1}, {2, 2}}, membersOf(chains[0]))
}

// An error repeated up to the next step is one record dated at its first
// occurrence; it still counts as happening within the window.
func TestFindChains_ARepeatedErrorCountsUntilItsLastOccurrence(t *testing.T) {
	timeouts := makeChainEntry("timeout connecting to payment-api", types.LevelError, "app", chainBase, 1)
	timeouts.FirstSeen, timeouts.LastSeen, timeouts.OccurrenceCount = chainBase, chainBase.Add(290*time.Second), 30
	breaker := makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "app", chainBase.Add(295*time.Second), 2)

	chains := NewChainMatcher().FindChains([]types.EnrichedEntry{timeouts, breaker})

	require.Len(t, chains, 1)
	assert.Equal(t, []step{{0, 1}, {1, 2}}, membersOf(chains[0]))
}

// A chain stays open for its window after its latest new step, so repeats
// right after the last step join it; later ones don't, so a day of timeouts
// doesn't become one chain.
func TestFindChains_RepeatsJoinUntilTheWindowCloses(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("timeout connecting to payment-api", types.LevelError, "app", chainBase, 1),
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "app", chainBase.Add(5*time.Second), 2),
		makeChainEntry("fallback activated: cached prices", types.LevelWarn, "app", chainBase.Add(6*time.Second), 3),
		makeChainEntry("circuit breaker open for ledger", types.LevelWarn, "app", chainBase.Add(8*time.Second), 4),
	}
	for k := range 60 {
		entries = append(entries, makeChainEntry(fmt.Sprintf("timeout reading shard %c", 'a'+k%26), types.LevelError, "app",
			chainBase.Add(time.Duration(k+1)*time.Minute), int64(5+k)))
	}

	chains := NewChainMatcher().FindChains(entries)

	require.NotEmpty(t, chains)
	assert.Equal(t, "circuit-breaker", chains[0].ID)
	assert.Equal(t, []step{{0, 1}, {1, 2}, {2, 3}, {3, 2}, {4, 1}, {5, 1}}, membersOf(chains[0]),
		"the breaker for ledger joins, and the timeouts at 1m and 2m, within 2 minutes of the last new step (6s)")
}

// An entry can belong to chains of different patterns; each keeps it.
func TestFindChains_AnEntryCanBelongToTwoChains(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("DNS lookup failed for payments.internal", types.LevelError, "app", chainBase, 1),
		makeChainEntry("dial tcp 10.0.0.5:443: connect: ECONNREFUSED", types.LevelError, "app", chainBase.Add(2*time.Second), 2),
		makeChainEntry("circuit breaker open for payments", types.LevelWarn, "app", chainBase.Add(5*time.Second), 3),
		makeChainEntry("fallback activated: serving cached prices", types.LevelWarn, "app", chainBase.Add(6*time.Second), 4),
	}

	chains := NewChainMatcher().FindChains(entries)

	byPattern := map[string][]step{}
	for _, c := range chains {
		byPattern[c.Pattern] = membersOf(c)
	}
	assert.Equal(t, []step{{0, 1}, {1, 2}}, byPattern["dns-failure"])
	assert.Equal(t, []step{{1, 1}, {2, 2}, {3, 3}}, byPattern["circuit-breaker"])
}

func TestFindChains_InputOrderDoesNotMatter(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "app", chainBase.Add(10*time.Second), 1),
		makeChainEntry("timeout connecting to payment-api", types.LevelError, "app", chainBase, 2),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 1)
	assert.Equal(t, []step{{1, 1}, {0, 2}}, membersOf(chains[0]))
}

// The deployment-failure chain starts at a deployment, as the deploy
// detector sees it, so every start it names survives the level filter too.
func TestFindChains_DeploymentFailureStartsAtADeployment(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("new version 2.4.1 rolled to production", types.LevelInfo, "deploy", chainBase, 1),
		makeChainEntry("readiness probe failed: HTTP 503", types.LevelWarn, "k8s", chainBase.Add(time.Minute), 2),
		makeChainEntry("rollback triggered", types.LevelError, "deploy", chainBase.Add(2*time.Minute), 3),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 1)
	assert.Equal(t, "deployment-failure", chains[0].Pattern)
	assert.Equal(t, 3, chains[0].Matched)
}

// A chain begins at its lowest step: errors of later steps that ended
// before it are not part of it, so the timeline never runs backwards.
func TestFindChains_ChainStartsAtItsLowestStep(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("upstream service unavailable after 3 retries", types.LevelError, "web", chainBase, 1),
		makeChainEntry("service unavailable: proxy returned 503", types.LevelError, "web", chainBase.Add(8*time.Second), 2),
		makeChainEntry("OOM killer invoked", types.LevelFatal, "kernel", chainBase.Add(15*time.Second), 3),
		makeChainEntry("service unavailable, shutting down", types.LevelFatal, "app", chainBase.Add(15*time.Second), 4),
	}

	chains := NewChainMatcher().FindChains(entries)

	require.Len(t, chains, 1)
	assert.Equal(t, "oom-cascade", chains[0].Pattern)
	assert.Equal(t, []step{{2, 2}, {3, 3}}, membersOf(chains[0]))
}

// A rollback is a deployment marker, but it ends a deployment-failure chain;
// it doesn't start one.
func TestFindChains_ARollbackIsNotTheFirstStep(t *testing.T) {
	entries := []types.EnrichedEntry{
		makeChainEntry("rolling back deployment to v2.3.9", types.LevelInfo, "deploy", chainBase, 1),
		makeChainEntry("readiness probe failed: HTTP 503", types.LevelWarn, "k8s", chainBase.Add(time.Minute), 2),
	}

	assert.Empty(t, NewChainMatcher().FindChains(entries))
}

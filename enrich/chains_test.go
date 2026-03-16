package enrich

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestChainMatcher_DBConnectionExhaustion(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("deadlock detected on table users", types.LevelWarn, "db", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "db-connection-exhaustion")

	e2 := makeChainEntry("connection pool exhausted, 0 available", types.LevelError, "db", base.Add(30*time.Second), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("query failed: cannot execute SELECT", types.LevelError, "app", base.Add(60*time.Second), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_OOMCascade(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("memory warning: heap usage exceeds 90%", types.LevelWarn, "app", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1)

	e2 := makeChainEntry("OOM killed process 1234", types.LevelError, "kernel", base.Add(time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2)

	e3 := makeChainEntry("pod restart: app-server-xyz", types.LevelError, "k8s", base.Add(90*time.Second), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3)
}

func TestChainMatcher_DeploymentFailure(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("deployment starting for api-server v2.4.1, pulling image", types.LevelInfo, "deploy", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "deployment-failure")

	e2 := makeChainEntry("health check failed for api-server: HTTP 503", types.LevelWarn, "k8s", base.Add(3*time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("rollback initiated: reverting api-server to v2.3.9", types.LevelError, "deploy", base.Add(5*time.Minute), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_CircuitBreaker(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("timeout connecting to payment-api after 30s", types.LevelError, "gateway", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "circuit-breaker")

	e2 := makeChainEntry("circuit breaker open for payment-api", types.LevelWarn, "gateway", base.Add(20*time.Second), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("fallback activated: returning cached response", types.LevelWarn, "gateway", base.Add(40*time.Second), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_DiskFull(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("disk space warning: /data partition at 95%", types.LevelWarn, "monitor", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "disk-full")

	e2 := makeChainEntry("write failed: no space left on device", types.LevelError, "db", base.Add(2*time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("fatal: service terminated due to unrecoverable error", types.LevelError, "db", base.Add(3*time.Minute), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_CertificateExpiry(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("certificate expiring in 2 days for api.example.com", types.LevelWarn, "cert-manager", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "certificate-expiry")

	e2 := makeChainEntry("TLS handshake failed: certificate expired", types.LevelError, "nginx", base.Add(2*time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("connection refused from downstream client 10.0.1.5", types.LevelError, "nginx", base.Add(3*time.Minute), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_DNSFailure(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("DNS lookup failed for payments.internal.svc", types.LevelError, "resolver", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "dns-failure")

	e2 := makeChainEntry("connection failed: dial tcp: lookup payments.internal.svc: no such host", types.LevelError, "app", base.Add(30*time.Second), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("upstream unavailable: payments service not reachable", types.LevelError, "gateway", base.Add(60*time.Second), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_RateLimiting(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("rate limit warning: API calls approaching threshold", types.LevelWarn, "api", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "rate-limiting")

	e2 := makeChainEntry("HTTP 429 too many requests from client 10.0.0.5", types.LevelError, "api", base.Add(time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("client error: multiple requests failed due to throttling", types.LevelError, "client", base.Add(2*time.Minute), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_QueueBacklog(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("consumer lag increasing: orders-queue 50000 messages behind", types.LevelWarn, "kafka", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "queue-backlog")

	e2 := makeChainEntry("queue full: orders-queue cannot accept new messages, message rejected", types.LevelError, "kafka", base.Add(5*time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("producer blocked: send timeout after 30s waiting for capacity", types.LevelError, "order-service", base.Add(7*time.Minute), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_CascadeFailure(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeChainEntry("error: failed to process request in auth service", types.LevelError, "auth", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1, "stage 0 should start chain")
	assert.Contains(t, id1, "cascade-failure")

	e2 := makeChainEntry("request timed out waiting for upstream auth response", types.LevelError, "gateway", base.Add(time.Minute), 2)
	id2 := cm.Match(&e2)
	assert.Equal(t, id1, id2, "stage 1 should match same chain")

	e3 := makeChainEntry("HTTP 502 error returned to client for /api/checkout", types.LevelError, "gateway", base.Add(2*time.Minute), 3)
	id3 := cm.Match(&e3)
	assert.Equal(t, id1, id3, "stage 2 should complete chain")
}

func TestChainMatcher_Expiration(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Start a DB chain (5m window).
	e1 := makeChainEntry("deadlock detected", types.LevelWarn, "db", base, 1)
	cm.Match(&e1)
	assert.Len(t, cm.active, 1)

	// Entry 6 minutes later should expire the chain.
	e2 := makeChainEntry("unrelated error", types.LevelError, "app", base.Add(6*time.Minute), 2)
	cm.Match(&e2)
	// The db-connection-exhaustion chain should be expired.
	for _, ac := range cm.active {
		assert.NotContains(t, ac.ID, "db-connection-exhaustion")
	}
}

func TestChainMatcher_NoMatch(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e := makeChainEntry("GET /api/users 200 OK", types.LevelInfo, "app", base, 1)
	id := cm.Match(&e)
	assert.Empty(t, id)
}

func TestChainMatcher_LevelFilter(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// DB chain stage 0 requires Warn. Send as Info — should not match.
	e := makeChainEntry("deadlock detected", types.LevelInfo, "db", base, 1)
	id := cm.Match(&e)
	assert.Empty(t, id)
}

func TestChainMatcher_OverlappingChains(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Start two DB chains.
	e1 := makeChainEntry("deadlock detected", types.LevelWarn, "db1", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1)

	e2 := makeChainEntry("lock timeout on index", types.LevelWarn, "db2", base.Add(10*time.Second), 2)
	id2 := cm.Match(&e2)
	require.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2, "should be different chains")
}

func TestChainMatcher_MultiplePatternMatch(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// "connection refused" matches circuit-breaker stage 0 AND certificate-expiry stage 2.
	// "failed ... service" matches cascade-failure stage 0.
	// Send a message that triggers multiple stage-0 matches.
	e1 := makeChainEntry("error failed to reach service, connection refused", types.LevelError, "app", base, 1)
	id := cm.Match(&e1)
	require.NotEmpty(t, id)
	// Should have created multiple active chains (circuit-breaker + cascade-failure at minimum).
	assert.GreaterOrEqual(t, len(cm.active), 2)
}

func TestChainMatcher_CompletedChainThenNewMatch(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Complete a full DB chain.
	e1 := makeChainEntry("deadlock detected", types.LevelWarn, "db", base, 1)
	id1 := cm.Match(&e1)
	e2 := makeChainEntry("connection pool exhausted", types.LevelError, "db", base.Add(30*time.Second), 2)
	cm.Match(&e2)
	e3 := makeChainEntry("query failed: cannot execute", types.LevelError, "db", base.Add(60*time.Second), 3)
	cm.Match(&e3)

	// Now another deadlock starts a NEW chain — completed chain should not interfere.
	e4 := makeChainEntry("deadlock detected on index", types.LevelWarn, "db", base.Add(90*time.Second), 4)
	id4 := cm.Match(&e4)
	require.NotEmpty(t, id4)
	assert.NotEqual(t, id1, id4, "should be a new chain, not the completed one")
}

func TestChainMatcher_ZeroTimestamp(t *testing.T) {
	cm := NewChainMatcher()
	zero := time.Time{}

	e := makeChainEntry("deadlock detected", types.LevelWarn, "db", zero, 1)
	id := cm.Match(&e)
	// Should still match — zero timestamps are valid, just unusual.
	assert.NotEmpty(t, id)
}

func TestChainMatcher_EmptyMessage(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e := makeChainEntry("", types.LevelError, "app", base, 1)
	id := cm.Match(&e)
	assert.Empty(t, id, "empty message should not match any chain pattern")
}

func TestChainMatcher_ActiveChainCap(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Flood with entries that match cascade-failure stage 0.
	for i := 0; i < 150; i++ {
		e := makeChainEntry("failed to reach service endpoint", types.LevelError, "app", base.Add(time.Duration(i)*time.Second), int64(i))
		cm.Match(&e)
	}

	// Count active chains for cascade-failure — should be capped at 100.
	count := 0
	for _, ac := range cm.active {
		if ac.Pattern.Name == "cascade-failure" {
			count++
		}
	}
	assert.LessOrEqual(t, count, 100, "active chains per pattern should be capped")
}

func TestChainMatcher_HighWaterMark(t *testing.T) {
	cm := NewChainMatcher()
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Start a circuit breaker chain (2m window).
	e1 := makeChainEntry("timeout connecting to payment-api", types.LevelError, "app", base, 1)
	id1 := cm.Match(&e1)
	require.NotEmpty(t, id1)

	// Jump forward 3 minutes (past the window).
	e2 := makeChainEntry("unrelated info", types.LevelError, "other", base.Add(3*time.Minute), 2)
	cm.Match(&e2)

	// Now send an out-of-order entry with timestamp within the original window.
	// The chain should still be expired because maxTime advanced past the window.
	e3 := makeChainEntry("circuit breaker open", types.LevelWarn, "app", base.Add(30*time.Second), 3)
	id3 := cm.Match(&e3)
	// Should NOT advance the expired chain — either empty or a different chain ID.
	assert.NotEqual(t, id1, id3)
}

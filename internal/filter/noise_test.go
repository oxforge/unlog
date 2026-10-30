package filter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oxforge/unlog/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoiseFilter(t *testing.T) {
	t.Run("built-in patterns match health checks", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		noisy := []string{
			"GET /healthz 200 OK",
			"GET /health returned 200",
			"GET /ready check",
			"GET /readyz passed",
			"GET /livez check",
			"GET /live is fine",
			"GET /ping response",
			"GET /status returned OK",
			"health check passed for service-a",
		}
		for _, msg := range noisy {
			entry := types.LogEntry{Message: msg}
			assert.False(t, f.Filter(entry), "should drop: %s", msg)
		}
	})

	t.Run("built-in patterns match prometheus", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		noisy := []string{
			"GET /metrics 200 12ms",
			"prometheus scrape completed",
		}
		for _, msg := range noisy {
			entry := types.LogEntry{Message: msg}
			assert.False(t, f.Filter(entry), "should drop: %s", msg)
		}
	})

	t.Run("built-in patterns match kubernetes internals", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		noisy := []string{
			"leader election acquired lock",
			"lease renewed for node-1",
			"Successfully synced configmap",
		}
		for _, msg := range noisy {
			entry := types.LogEntry{Message: msg}
			assert.False(t, f.Filter(entry), "should drop: %s", msg)
		}
	})

	t.Run("built-in patterns match TLS noise", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		noisy := []string{
			"TLS handshake complete with peer",
			"certificate verified for domain.com",
		}
		for _, msg := range noisy {
			entry := types.LogEntry{Message: msg}
			assert.False(t, f.Filter(entry), "should drop: %s", msg)
		}
	})

	t.Run("built-in patterns match connection pool", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		noisy := []string{
			"connection pool stats: active=5 idle=10",
			"idle connections: 15",
		}
		for _, msg := range noisy {
			entry := types.LogEntry{Message: msg}
			assert.False(t, f.Filter(entry), "should drop: %s", msg)
		}
	})

	t.Run("real errors are kept", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		kept := []string{
			"ERROR: database connection timeout after 30s",
			"FATAL: out of memory, killing process",
			"connection refused to payment-service:8080",
			"panic: runtime error: index out of range",
			"OOM killed container app-server",
		}
		for _, msg := range kept {
			entry := types.LogEntry{Message: msg}
			assert.True(t, f.Filter(entry), "should keep: %s", msg)
		}
	})

	t.Run("errors are never noise", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		for _, level := range []types.Level{types.LevelError, types.LevelFatal} {
			for _, msg := range []string{
				"GET /healthz 503 upstream unavailable",
				"retrying request failed permanently after 5 attempts",
				"idle connections exhausted",
			} {
				entry := types.LogEntry{Level: level, Message: msg}
				assert.True(t, f.Filter(entry), "should keep %s: %s", level, msg)
			}
		}
	})

	t.Run("noise below error is still dropped", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		for _, level := range []types.Level{types.LevelUnknown, types.LevelInfo, types.LevelWarn} {
			entry := types.LogEntry{Level: level, Message: "retrying request to s3 (attempt 2)"}
			assert.False(t, f.Filter(entry), "should drop at %s", level)
		}
	})

	t.Run("broad phrases are not noise", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		for _, msg := range []string{
			"max pool size reached, rejecting connections",
			"failed to push /metrics to gateway: 500",
			"Back-off restarting failed container api in pod api-7f9c",
			"BackOff restarting failed container",
		} {
			entry := types.LogEntry{Level: types.LevelWarn, Message: msg}
			assert.True(t, f.Filter(entry), "should keep: %s", msg)
		}
	})

	t.Run("empty message is kept", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)

		entry := types.LogEntry{Message: ""}
		assert.True(t, f.Filter(entry))
	})

	t.Run("custom noise file works", func(t *testing.T) {
		dir := t.TempDir()
		customFile := filepath.Join(dir, "custom_noise.txt")
		err := os.WriteFile(customFile, []byte("# Custom patterns\ncustom noise pattern\nmy special debug\n"), 0644)
		require.NoError(t, err)

		f, err := NewNoiseFilter(customFile)
		require.NoError(t, err)

		// Custom patterns should be dropped
		entry := types.LogEntry{Message: "this has custom noise pattern in it"}
		assert.False(t, f.Filter(entry))

		entry = types.LogEntry{Message: "my special debug output here"}
		assert.False(t, f.Filter(entry))

		// Real errors still kept
		entry = types.LogEntry{Message: "ERROR: something broke"}
		assert.True(t, f.Filter(entry))
	})

	t.Run("built-in patterns still work with custom file", func(t *testing.T) {
		dir := t.TempDir()
		customFile := filepath.Join(dir, "custom_noise.txt")
		err := os.WriteFile(customFile, []byte("extra pattern\n"), 0644)
		require.NoError(t, err)

		f, err := NewNoiseFilter(customFile)
		require.NoError(t, err)

		// Built-in still works
		entry := types.LogEntry{Message: "GET /healthz 200"}
		assert.False(t, f.Filter(entry))

		// Custom also works
		entry = types.LogEntry{Message: "extra pattern found"}
		assert.False(t, f.Filter(entry))
	})

	t.Run("Name returns noise", func(t *testing.T) {
		f, err := NewNoiseFilter("")
		require.NoError(t, err)
		assert.Equal(t, "noise", f.Name())
	})
}

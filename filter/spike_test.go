package filter

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func makeFiltered(msg string, ts time.Time, source string) types.FilteredEntry {
	return types.FilteredEntry{
		LogEntry: types.LogEntry{Timestamp: ts, Level: types.LevelError, Source: source, Message: msg},
	}
}

func TestDetectSpikes(t *testing.T) {
	t.Run("BasicSpike", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		var entries []types.FilteredEntry

		// 30 entries at 1/sec normal rate (seconds 0-29)
		for i := 0; i < 30; i++ {
			entries = append(entries, makeFiltered("normal", base.Add(time.Duration(i)*time.Second), "app"))
		}

		// 20 entries all in the same second (second 30) → spike
		for i := 0; i < 20; i++ {
			entries = append(entries, makeFiltered("spike", base.Add(30*time.Second), "app"))
		}

		count := DetectSpikes(entries, 10)

		// All 20 spike entries should be flagged
		assert.Equal(t, int64(20), count)

		// Verify spike entries are flagged
		for i := 30; i < 50; i++ {
			assert.True(t, entries[i].IsSpike, "entry %d should be spike", i)
		}

		// Verify normal entries are not flagged
		for i := 0; i < 30; i++ {
			assert.False(t, entries[i].IsSpike, "entry %d should not be spike", i)
		}
	})

	t.Run("TooFewEntries", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		entries := []types.FilteredEntry{
			makeFiltered("one", base, "app"),
			makeFiltered("two", base.Add(time.Second), "app"),
		}

		count := DetectSpikes(entries, 10)
		assert.Equal(t, int64(0), count)
	})

	t.Run("ZeroTimestamp", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		var entries []types.FilteredEntry

		// Mix of zero and valid timestamps
		entries = append(entries, makeFiltered("no-ts", time.Time{}, "app"))
		for i := 0; i < 30; i++ {
			entries = append(entries, makeFiltered("normal", base.Add(time.Duration(i)*time.Second), "app"))
		}
		for i := 0; i < 20; i++ {
			entries = append(entries, makeFiltered("spike", base.Add(30*time.Second), "app"))
		}

		count := DetectSpikes(entries, 10)

		// Zero-timestamp entry should not be flagged
		assert.False(t, entries[0].IsSpike)
		// Spike entries should still be detected
		assert.Equal(t, int64(20), count)
	})

	t.Run("MultipleSourcesIndependent", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		var entries []types.FilteredEntry

		// source-a: normal rate, 30 entries at 1/sec
		for i := 0; i < 30; i++ {
			entries = append(entries, makeFiltered("normal-a", base.Add(time.Duration(i)*time.Second), "source-a"))
		}

		// source-b: normal then spike
		for i := 0; i < 30; i++ {
			entries = append(entries, makeFiltered("normal-b", base.Add(time.Duration(i)*time.Second), "source-b"))
		}
		for i := 0; i < 20; i++ {
			entries = append(entries, makeFiltered("spike-b", base.Add(30*time.Second), "source-b"))
		}

		count := DetectSpikes(entries, 10)

		// Only source-b spike entries should be flagged
		assert.Equal(t, int64(20), count)

		// source-a entries: none flagged
		for i := 0; i < 30; i++ {
			assert.False(t, entries[i].IsSpike, "source-a entry %d should not be spike", i)
		}

		// source-b normal entries: not flagged
		for i := 30; i < 60; i++ {
			assert.False(t, entries[i].IsSpike, "source-b normal entry %d should not be spike", i)
		}

		// source-b spike entries: flagged
		for i := 60; i < 80; i++ {
			assert.True(t, entries[i].IsSpike, "source-b spike entry %d should be spike", i)
		}
	})
}

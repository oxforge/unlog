package filter

import (
	"sync"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func makeEntry(msg string, ts time.Time) types.LogEntry {
	return types.LogEntry{Timestamp: ts, Level: types.LevelError, Source: "test", Message: msg}
}

func TestDedupFilter_KeepsFirstN(t *testing.T) {
	df := NewDedupFilter(2, 1, 100)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Same message 3 times — first 2 kept, 3rd dropped.
	r1, ok1 := df.Apply(makeEntry("error on host 10.0.0.1", ts))
	assert.True(t, ok1)
	assert.Equal(t, 1, r1.OccurrenceCount)

	r2, ok2 := df.Apply(makeEntry("error on host 10.0.0.2", ts.Add(time.Second)))
	assert.True(t, ok2)
	assert.Equal(t, 2, r2.OccurrenceCount)

	_, ok3 := df.Apply(makeEntry("error on host 10.0.0.3", ts.Add(2*time.Second)))
	assert.False(t, ok3)
}

func TestDedupFilter_DifferentSignaturesIndependent(t *testing.T) {
	df := NewDedupFilter(1, 1, 100)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	r1, ok1 := df.Apply(makeEntry("connection refused", ts))
	assert.True(t, ok1)
	assert.Equal(t, 1, r1.OccurrenceCount)

	r2, ok2 := df.Apply(makeEntry("disk full", ts.Add(time.Second)))
	assert.True(t, ok2)
	assert.Equal(t, 1, r2.OccurrenceCount)
}

func TestDedupFilter_Summaries(t *testing.T) {
	df := NewDedupFilter(1, 1, 100)
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	t2 := t0.Add(2 * time.Second)

	msg1 := "error on host 10.0.0.1"
	msg2 := "error on host 10.0.0.2"
	msg3 := "error on host 10.0.0.3"

	// Same signature, different variable parts.
	df.Apply(makeEntry(msg1, t0))
	df.Apply(makeEntry(msg2, t1))
	df.Apply(makeEntry(msg3, t2))

	summaries := df.Summaries()
	assert.Len(t, summaries, 1)

	s := summaries[0]
	assert.Equal(t, 3, s.OccurrenceCount)
	assert.Equal(t, t0, s.FirstSeen)
	assert.Equal(t, t2, s.LastSeen)
	assert.Equal(t, types.LevelError, s.Level)
	assert.Equal(t, "test", s.Source)
	// Message should be the original first-seen message, not the signature.
	assert.Equal(t, msg1, s.Message)
}

func TestDedupFilter_LRUEviction(t *testing.T) {
	// 1 shard, cache size 2 — third distinct signature evicts the first.
	df := NewDedupFilter(5, 1, 2)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	df.Apply(makeEntry("alpha error", ts))
	df.Apply(makeEntry("beta error", ts.Add(time.Second)))
	df.Apply(makeEntry("gamma error", ts.Add(2*time.Second)))

	// "alpha error" was evicted; re-applying should return count=1.
	r, ok := df.Apply(makeEntry("alpha error", ts.Add(3*time.Second)))
	assert.True(t, ok)
	assert.Equal(t, 1, r.OccurrenceCount)
}

func TestDedupFilter_ConcurrentAccess(t *testing.T) {
	df := NewDedupFilter(5, 16, 1000)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			df.Apply(makeEntry("concurrent error on host 10.0.0.1", ts))
		}()
	}
	wg.Wait()

	// Just verify no panic/race and unique signatures is 1.
	assert.Equal(t, 1, df.UniqueSignatures())
}

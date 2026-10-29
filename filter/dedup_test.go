package filter

import (
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

func entryAt(level types.Level, source, msg string, ts time.Time, line int64) types.LogEntry {
	return types.LogEntry{Timestamp: ts, Level: level, Source: source, Message: msg, LineNumber: line}
}

func TestDedupMergesRepeatsIntoOneRecord(t *testing.T) {
	const n = 3000
	entries := make([]types.LogEntry, n)
	for i := range entries {
		entries[i] = entryAt(types.LevelError, "app.log", "upstream timeout for order "+strconv.Itoa(i),
			t0.Add(time.Duration(i)*time.Second), int64(i+1))
	}
	rand.New(rand.NewSource(42)).Shuffle(n, func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })

	df := NewDedupFilter(4)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := g; i < n; i += 4 {
				df.Add(entries[i])
			}
		}()
	}
	wg.Wait()

	got := df.Entries()
	require.Len(t, got, 1)
	assert.Equal(t, 3000, got[0].OccurrenceCount)
	assert.True(t, t0.Equal(got[0].FirstSeen))
	assert.True(t, t0.Add(2999*time.Second).Equal(got[0].LastSeen), "last seen 10:49:59")
	assert.Equal(t, "upstream timeout for order 0", got[0].Message)
	assert.True(t, got[0].FirstSeen.Equal(got[0].Timestamp))
}

func TestDedupKeepsHighestLevel(t *testing.T) {
	df := NewDedupFilter(1)
	df.Add(entryAt(types.LevelWarn, "a.log", "disk 91% full", t0, 1))
	df.Add(entryAt(types.LevelError, "a.log", "disk 97% full", t0.Add(5*time.Minute), 2))
	got := df.Entries()
	require.Len(t, got, 1)
	assert.Equal(t, types.LevelError, got[0].Level)
	assert.Equal(t, "disk 91% full", got[0].Message)
}

func TestDedupSeparatesSources(t *testing.T) {
	df := NewDedupFilter(4)
	df.Add(entryAt(types.LevelError, "api-1.log", "connection refused", t0, 1))
	df.Add(entryAt(types.LevelError, "api-2.log", "connection refused", t0, 1))
	assert.Len(t, df.Entries(), 2)
}

func TestDedupTieBreaksOnLineNumber(t *testing.T) {
	df := NewDedupFilter(1)
	df.Add(entryAt(types.LevelError, "a.log", "boom", t0, 7))
	df.Add(entryAt(types.LevelError, "a.log", "boom", t0, 3))
	got := df.Entries()
	require.Len(t, got, 1)
	assert.Equal(t, int64(3), got[0].LineNumber)
}

func TestDedupUndatedNeverWinsOverDated(t *testing.T) {
	df := NewDedupFilter(1)
	df.Add(entryAt(types.LevelError, "a.log", "boom", time.Time{}, 1))
	df.Add(entryAt(types.LevelError, "a.log", "boom", t0, 9))
	got := df.Entries()
	require.Len(t, got, 1)
	assert.Equal(t, int64(9), got[0].LineNumber)
	assert.True(t, t0.Equal(got[0].FirstSeen))
	assert.Equal(t, 2, got[0].OccurrenceCount)
}

func TestDedupEntriesAreSorted(t *testing.T) {
	df := NewDedupFilter(2)
	df.Add(entryAt(types.LevelError, "b.log", "second", t0.Add(2*time.Minute), 1))
	df.Add(entryAt(types.LevelError, "a.log", "first", t0.Add(time.Minute), 1))
	df.Add(entryAt(types.LevelError, "c.log", "undated", time.Time{}, 1))
	got := df.Entries()
	require.Len(t, got, 3)
	assert.Equal(t, "first", got[0].Message)
	assert.Equal(t, "second", got[1].Message)
	assert.Equal(t, "undated", got[2].Message)
}

func TestUniqueSignaturesIgnoresSource(t *testing.T) {
	df := NewDedupFilter(4)
	df.Add(entryAt(types.LevelError, "a.log", "connection refused", t0, 1))
	df.Add(entryAt(types.LevelError, "b.log", "connection refused", t0, 1))
	df.Add(entryAt(types.LevelError, "a.log", "disk full", t0, 2))
	assert.Equal(t, 2, df.UniqueSignatures())
}

func TestDedupSpikeMarkAppearsInEntries(t *testing.T) {
	df := NewDedupFilter(1)
	id := df.Add(entryAt(types.LevelError, "a.log", "boom", t0, 1))
	require.NotNil(t, df.record(id))
	df.markSpike(id)
	got := df.Entries()
	require.Len(t, got, 1)
	assert.True(t, got[0].IsSpike)
}

func TestDedupCountsOccurrencesPerLevel(t *testing.T) {
	d := NewDedupFilter(4)
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	d.Add(types.LogEntry{Timestamp: at, Level: types.LevelWarn, Source: "app.log", Message: "disk 91% full"})
	d.Add(types.LogEntry{Timestamp: at.Add(5 * time.Minute), Level: types.LevelError, Source: "app.log", Message: "disk 97% full"})
	d.Add(types.LogEntry{Timestamp: at.Add(6 * time.Minute), Level: types.LevelError, Source: "app.log", Message: "disk 98% full"})

	entries := d.Entries()

	require.Len(t, entries, 1)
	assert.Equal(t, map[types.Level]int{types.LevelWarn: 1, types.LevelError: 2}, entries[0].OccurrencesByLevel)
}

func TestDedupSplitsServicesWithinAFile(t *testing.T) {
	d := NewDedupFilter(4)
	at := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	d.Add(types.LogEntry{Timestamp: at, Level: types.LevelError, Source: "agg.log", Service: "payments", Message: "upstream refused"})
	d.Add(types.LogEntry{Timestamp: at, Level: types.LevelError, Source: "agg.log", Service: "search", Message: "upstream refused"})
	d.Add(types.LogEntry{Timestamp: at.Add(time.Second), Level: types.LevelError, Source: "agg.log", Service: "payments", Message: "upstream refused"})

	entries := d.Entries()

	require.Len(t, entries, 2)
	assert.Equal(t, "payments", entries[0].Service)
	assert.Equal(t, 2, entries[0].OccurrenceCount)
	assert.Equal(t, "search", entries[1].Service)
}

package enrich

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/types"
	"github.com/stretchr/testify/assert"
)

var corrBase = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

// failure is an ERROR from origin ("db.log", or "agg.log[payments]" for a
// service in a file) at offset from corrBase.
func failure(origin string, offset time.Duration) types.EnrichedEntry {
	e := makeEnrichedEntry("failure in "+origin, nil)
	e.Timestamp = corrBase.Add(offset)
	e.Source = origin
	if source, service, ok := strings.Cut(origin, "["); ok {
		e.Source, e.Service = source, strings.TrimSuffix(service, "]")
	}
	return e
}

func correlate(entries ...types.EnrichedEntry) []types.EnrichedEntry {
	NewCorrelator(5 * time.Second).Correlate(entries)
	return entries
}

// Both errors of a pair are tagged, so the first failure of a cascade (the
// likeliest root cause) gets the tag and its score too.
func TestCorrelator_TagsBothSidesOfAPair(t *testing.T) {
	got := correlate(failure("db.log", 0), failure("api.log", 2*time.Second))

	assert.Equal(t, []string{"api.log"}, got[0].CorrelatedWith)
	assert.Equal(t, []string{"db.log"}, got[1].CorrelatedWith)
	assert.Equal(t, 1, got[0].CorrelatedCount)
}

func TestCorrelator_Window(t *testing.T) {
	for name, tt := range map[string]struct {
		gap  time.Duration
		want []string
	}{
		"at the edge":     {5 * time.Second, []string{"api.log"}},
		"just past it":    {5*time.Second + time.Millisecond, nil},
		"10s with 5s":     {10 * time.Second, nil},
		"the same second": {0, []string{"api.log"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := correlate(failure("db.log", 0), failure("api.log", tt.gap))
			assert.Equal(t, tt.want, got[0].CorrelatedWith)
		})
	}
}

func TestCorrelator_SameOriginIsNotConcurrent(t *testing.T) {
	got := correlate(failure("api.log", 0), failure("api.log", time.Second))

	assert.Nil(t, got[0].CorrelatedWith)
	assert.Nil(t, got[1].CorrelatedWith)
}

// Undated errors have no time to compare; they are neither tagged nor
// counted for others.
func TestCorrelator_UndatedErrorsAreLeftOut(t *testing.T) {
	undatedA, undatedB := failure("a.log", 0), failure("b.log", 0)
	undatedA.Timestamp, undatedB.Timestamp = time.Time{}, time.Time{}

	got := correlate(undatedA, undatedB, failure("c.log", 0))

	for i := range got {
		assert.Nil(t, got[i].CorrelatedWith, got[i].Source)
	}
}

// Services in one file fail together like separate files do, but a line
// that names no service may come from any of them, so it isn't counted
// against the services in its own file.
func TestCorrelator_ServicesWithinAFile(t *testing.T) {
	got := correlate(
		failure("agg.log[payments]", 0),
		failure("agg.log[search]", time.Second),
		failure("agg.log", 2*time.Second),
		failure("db.log", 3*time.Second),
	)

	assert.Equal(t, []string{"agg.log[search]", "db.log"}, got[0].CorrelatedWith)
	assert.Equal(t, []string{"db.log"}, got[2].CorrelatedWith)
	assert.Equal(t, []string{"agg.log", "agg.log[payments]", "agg.log[search]"}, got[3].CorrelatedWith)
}

func TestCorrelator_OnlyErrorsCorrelate(t *testing.T) {
	warn, info, fatal := failure("db.log", 0), failure("web.log", time.Second), failure("disk.log", 2*time.Second)
	warn.Level, info.Level, fatal.Level = types.LevelWarn, types.LevelInfo, types.LevelFatal

	got := correlate(warn, info, fatal, failure("api.log", 3*time.Second))

	assert.Nil(t, got[0].CorrelatedWith, "a WARN is not a failure")
	assert.Nil(t, got[1].CorrelatedWith, "an INFO line gets no correlation")
	assert.Equal(t, []string{"disk.log"}, got[3].CorrelatedWith, "FATAL counts")
}

// The tag lists the first origins by name and counts all of them, so a
// storm across many services costs a few strings per entry, not hundreds.
func TestCorrelator_ListsAFewAndCountsAll(t *testing.T) {
	var entries []types.EnrichedEntry
	for i := range 6 {
		entries = append(entries, failure(fmt.Sprintf("svc-%d.log", i), time.Duration(i)*100*time.Millisecond))
	}

	got := correlate(entries...)

	assert.Equal(t, []string{"svc-1.log", "svc-2.log", "svc-3.log"}, got[0].CorrelatedWith)
	assert.Equal(t, 5, got[0].CorrelatedCount)
}

func TestCorrelator_InputOrderDoesNotMatter(t *testing.T) {
	forward := correlate(failure("a.log", 0), failure("b.log", time.Second), failure("c.log", 8*time.Second))
	backward := correlate(failure("c.log", 8*time.Second), failure("b.log", time.Second), failure("a.log", 0))

	assert.Equal(t, forward[0].CorrelatedWith, backward[2].CorrelatedWith)
	assert.Equal(t, forward[1].CorrelatedWith, backward[1].CorrelatedWith)
	assert.Equal(t, forward[2].CorrelatedWith, backward[0].CorrelatedWith)
}

func BenchmarkCorrelate_DenseStorm(b *testing.B) {
	// 200 services × 100 distinct errors, all inside one window.
	entries := make([]types.EnrichedEntry, 0, 20_000)
	for s := range 200 {
		for e := range 100 {
			entries = append(entries, failure(fmt.Sprintf("agg.log[svc-%03d]", s), time.Duration(e)*time.Millisecond))
		}
	}
	c := NewCorrelator(5 * time.Second)
	b.ResetTimer()
	for range b.N {
		c.Correlate(entries)
	}
}

// A file literally named "x[y]" and service y in file x display alike; they
// are still two origins, and counting them as one must not corrupt the
// counts for later errors.
func TestCorrelator_OriginsThatDisplayAlikeStayApart(t *testing.T) {
	named := failure("x[y]", 0) // source x, service y
	file := failure("x[y]", time.Second)
	file.Source, file.Service = "x[y]", ""
	later := failure("x", 20*time.Second) // unnamed, in x

	got := correlate(named, file, failure("z.log", 2*time.Second), later, failure("z.log", 21*time.Second))

	assert.Equal(t, []string{"z.log"}, got[3].CorrelatedWith)
}

func TestCorrelator_NegativeWindowCorrelatesNothing(t *testing.T) {
	entries := []types.EnrichedEntry{failure("a.log", 0), failure("b.log", time.Second)}

	assert.NotPanics(t, func() { NewCorrelator(-time.Second).Correlate(entries) })
	assert.Nil(t, entries[0].CorrelatedWith)
}

package enrich

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCorrelator_WithinWindow(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeEnrichedEntry("error in db", nil)
	e1.Timestamp = base
	e1.Source = "db-svc"
	c.Correlate(&e1)
	assert.Nil(t, e1.CorrelatedWith, "first entry has no correlations")

	e2 := makeEnrichedEntry("error in api", nil)
	e2.Timestamp = base.Add(2 * time.Second)
	e2.Source = "api-svc"
	c.Correlate(&e2)
	assert.Contains(t, e2.CorrelatedWith, "db-svc")
}

func TestCorrelator_OutsideWindow(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeEnrichedEntry("error in db", nil)
	e1.Timestamp = base
	e1.Source = "db-svc"
	c.Correlate(&e1)

	e2 := makeEnrichedEntry("error in api", nil)
	e2.Timestamp = base.Add(10 * time.Second)
	e2.Source = "api-svc"
	c.Correlate(&e2)
	assert.Nil(t, e2.CorrelatedWith, "10s apart with 5s window — no correlation")
}

func TestCorrelator_SameSource(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeEnrichedEntry("error 1", nil)
	e1.Timestamp = base
	e1.Source = "api-svc"
	c.Correlate(&e1)

	e2 := makeEnrichedEntry("error 2", nil)
	e2.Timestamp = base.Add(1 * time.Second)
	e2.Source = "api-svc"
	c.Correlate(&e2)
	assert.Nil(t, e2.CorrelatedWith, "same source should not self-correlate")
}

func TestCorrelator_MultipleSources(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeEnrichedEntry("err", nil)
	e1.Timestamp = base
	e1.Source = "db"
	c.Correlate(&e1)

	e2 := makeEnrichedEntry("err", nil)
	e2.Timestamp = base.Add(1 * time.Second)
	e2.Source = "cache"
	c.Correlate(&e2)

	e3 := makeEnrichedEntry("err", nil)
	e3.Timestamp = base.Add(2 * time.Second)
	e3.Source = "api"
	c.Correlate(&e3)
	assert.Len(t, e3.CorrelatedWith, 2)
	assert.Contains(t, e3.CorrelatedWith, "db")
	assert.Contains(t, e3.CorrelatedWith, "cache")
}

func TestCorrelator_Eviction(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Add entry at t=0.
	e1 := makeEnrichedEntry("err", nil)
	e1.Timestamp = base
	e1.Source = "old-svc"
	c.Correlate(&e1)

	// Add entry at t=10s. Should evict old-svc.
	e2 := makeEnrichedEntry("err", nil)
	e2.Timestamp = base.Add(10 * time.Second)
	e2.Source = "new-svc"
	c.Correlate(&e2)

	// Buffer should only contain new-svc.
	assert.Equal(t, 1, len(c.recent))
}

func TestCorrelator_EmptySource(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	e1 := makeEnrichedEntry("err", nil)
	e1.Timestamp = base
	e1.Source = ""
	c.Correlate(&e1)

	e2 := makeEnrichedEntry("err", nil)
	e2.Timestamp = base.Add(1 * time.Second)
	e2.Source = ""
	c.Correlate(&e2)
	// Same (empty) source should not self-correlate.
	assert.Nil(t, e2.CorrelatedWith)
}

func TestCorrelator_ZeroTimestamp(t *testing.T) {
	c := NewCorrelator(5 * time.Second)

	e1 := makeEnrichedEntry("err", nil)
	e1.Timestamp = time.Time{}
	e1.Source = "svc-a"
	c.Correlate(&e1)

	e2 := makeEnrichedEntry("err", nil)
	e2.Timestamp = time.Time{}
	e2.Source = "svc-b"
	c.Correlate(&e2)
	// Both have zero timestamps — diff is 0 which is <= 5s window.
	assert.Contains(t, e2.CorrelatedWith, "svc-a")
}

func TestCorrelator_DeterministicOrder(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	sources := []string{"zebra", "alpha", "middle"}
	for i, src := range sources {
		e := makeEnrichedEntry("err", nil)
		e.Timestamp = base.Add(time.Duration(i) * time.Second)
		e.Source = src
		c.Correlate(&e)
	}

	// Last entry should have sorted CorrelatedWith.
	e := makeEnrichedEntry("err", nil)
	e.Timestamp = base.Add(3 * time.Second)
	e.Source = "test"
	c.Correlate(&e)
	assert.Equal(t, []string{"alpha", "middle", "zebra"}, e.CorrelatedWith)
}

func TestCorrelator_OutOfOrderTimestamp(t *testing.T) {
	c := NewCorrelator(5 * time.Second)
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Entry at t=0.
	e1 := makeEnrichedEntry("err", nil)
	e1.Timestamp = base
	e1.Source = "svc-a"
	c.Correlate(&e1)

	// Entry at t=10s — advances high-water mark past svc-a.
	e2 := makeEnrichedEntry("err", nil)
	e2.Timestamp = base.Add(10 * time.Second)
	e2.Source = "svc-b"
	c.Correlate(&e2)

	// Out-of-order entry at t=3s. svc-a should be evicted by now.
	e3 := makeEnrichedEntry("err", nil)
	e3.Timestamp = base.Add(3 * time.Second)
	e3.Source = "svc-c"
	c.Correlate(&e3)
	// svc-a evicted (maxTime=10, 10-0=10 > 5). svc-b within window of itself but not of e3 (|3-10|=7 > 5).
	assert.Nil(t, e3.CorrelatedWith)
}

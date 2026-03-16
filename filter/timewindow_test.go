package filter

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestTimeWindowFilter(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	since := base
	until := base.Add(1 * time.Hour)
	f := NewTimeWindowFilter(since, until)

	t.Run("within window kept", func(t *testing.T) {
		entry := types.LogEntry{Timestamp: base.Add(30 * time.Minute)}
		assert.True(t, f.Filter(entry))
	})

	t.Run("before window dropped", func(t *testing.T) {
		entry := types.LogEntry{Timestamp: base.Add(-1 * time.Minute)}
		assert.False(t, f.Filter(entry))
	})

	t.Run("after window dropped", func(t *testing.T) {
		entry := types.LogEntry{Timestamp: until.Add(1 * time.Minute)}
		assert.False(t, f.Filter(entry))
	})

	t.Run("since boundary inclusive", func(t *testing.T) {
		entry := types.LogEntry{Timestamp: since}
		assert.True(t, f.Filter(entry))
	})

	t.Run("until boundary inclusive", func(t *testing.T) {
		entry := types.LogEntry{Timestamp: until}
		assert.True(t, f.Filter(entry))
	})

	t.Run("since only", func(t *testing.T) {
		fSince := NewTimeWindowFilter(since, time.Time{})
		assert.False(t, fSince.Filter(types.LogEntry{Timestamp: base.Add(-1 * time.Minute)}))
		assert.True(t, fSince.Filter(types.LogEntry{Timestamp: base.Add(1 * time.Hour)}))
		assert.True(t, fSince.Filter(types.LogEntry{Timestamp: base.Add(100 * time.Hour)}))
	})

	t.Run("until only", func(t *testing.T) {
		fUntil := NewTimeWindowFilter(time.Time{}, until)
		assert.True(t, fUntil.Filter(types.LogEntry{Timestamp: base.Add(-100 * time.Hour)}))
		assert.True(t, fUntil.Filter(types.LogEntry{Timestamp: base.Add(30 * time.Minute)}))
		assert.False(t, fUntil.Filter(types.LogEntry{Timestamp: until.Add(1 * time.Second)}))
	})

	t.Run("no window set always keeps", func(t *testing.T) {
		fNone := NewTimeWindowFilter(time.Time{}, time.Time{})
		assert.True(t, fNone.Filter(types.LogEntry{Timestamp: base}))
		assert.True(t, fNone.Filter(types.LogEntry{Timestamp: base.Add(-1000 * time.Hour)}))
	})

	t.Run("zero timestamp always kept", func(t *testing.T) {
		entry := types.LogEntry{Timestamp: time.Time{}}
		assert.True(t, f.Filter(entry))
	})

	t.Run("IsActive false when no bounds", func(t *testing.T) {
		fNone := NewTimeWindowFilter(time.Time{}, time.Time{})
		assert.False(t, fNone.IsActive())
	})

	t.Run("IsActive true when bounds set", func(t *testing.T) {
		assert.True(t, f.IsActive())
	})

	t.Run("Name returns timewindow", func(t *testing.T) {
		assert.Equal(t, "timewindow", f.Name())
	})
}

package filter

import (
	"time"

	"github.com/oxforge/unlog/types"
)

// TimeWindowFilter drops entries outside a configured time range.
// Entries with zero timestamps are always kept.
type TimeWindowFilter struct {
	since time.Time
	until time.Time
}

// NewTimeWindowFilter creates a filter with the given bounds.
// Zero values for since or until mean that bound is not enforced.
func NewTimeWindowFilter(since, until time.Time) *TimeWindowFilter {
	return &TimeWindowFilter{since: since, until: until}
}

// IsActive returns true if at least one time bound is set.
func (f *TimeWindowFilter) IsActive() bool {
	return !f.since.IsZero() || !f.until.IsZero()
}

func (f *TimeWindowFilter) Filter(entry types.LogEntry) bool {
	if entry.Timestamp.IsZero() {
		return true
	}
	if !f.since.IsZero() && entry.Timestamp.Before(f.since) {
		return false
	}
	if !f.until.IsZero() && entry.Timestamp.After(f.until) {
		return false
	}
	return true
}

func (f *TimeWindowFilter) Name() string {
	return "timewindow"
}

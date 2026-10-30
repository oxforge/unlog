package filter

import "github.com/oxforge/unlog/internal/types"

// LevelFilter drops log entries below the configured minimum level.
// Entries with LevelUnknown are always kept (we cannot determine their severity).
type LevelFilter struct {
	minLevel types.Level
	// keep, when set, lets an entry through whatever its level.
	keep func(types.LogEntry) bool
}

// NewLevelFilter creates a LevelFilter that keeps entries at or above minLevel.
func NewLevelFilter(minLevel types.Level) *LevelFilter {
	return &LevelFilter{minLevel: minLevel}
}

func (f *LevelFilter) Filter(entry types.LogEntry) bool {
	if entry.Level == types.LevelUnknown || entry.Level.Meets(f.minLevel) {
		return true
	}
	return f.keep != nil && f.keep(entry)
}

func (f *LevelFilter) Name() string {
	return "level"
}

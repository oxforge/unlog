package filter

import "github.com/oxforge/unlog/types"

// LevelFilter drops log entries below the configured minimum level.
// Entries with LevelUnknown are always kept (we cannot determine their severity).
type LevelFilter struct {
	minLevel types.Level
}

// NewLevelFilter creates a LevelFilter that keeps entries at or above minLevel.
func NewLevelFilter(minLevel types.Level) *LevelFilter {
	return &LevelFilter{minLevel: minLevel}
}

func (f *LevelFilter) Filter(entry types.LogEntry) bool {
	return entry.Level == types.LevelUnknown || entry.Level.Meets(f.minLevel)
}

func (f *LevelFilter) Name() string {
	return "level"
}

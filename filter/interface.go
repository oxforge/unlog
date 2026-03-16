package filter

import "github.com/oxforge/unlog/types"

// EntryFilter is the interface for per-entry streaming filters.
// Implementations must be safe for concurrent use from multiple goroutines.
type EntryFilter interface {
	Filter(entry types.LogEntry) bool
	Name() string
}

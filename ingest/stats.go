package ingest

import (
	"sync"

	"github.com/oxforge/unlog/types"
)

// SourceStats holds per-source ingestion statistics.
type SourceStats struct {
	Format  string
	Entries int64
	Levels  map[types.Level]int64
}

// statsCollector is a thread-safe collector of per-source stats.
type statsCollector struct {
	mu      sync.Mutex
	sources map[string]*SourceStats
}

func newStatsCollector() *statsCollector {
	return &statsCollector{sources: make(map[string]*SourceStats)}
}

func (sc *statsCollector) register(source, format string) {
	sc.mu.Lock()
	sc.sources[source] = &SourceStats{
		Format: format,
		Levels: make(map[types.Level]int64),
	}
	sc.mu.Unlock()
}

func (sc *statsCollector) record(source string, level types.Level) {
	sc.mu.Lock()
	if s, ok := sc.sources[source]; ok {
		s.Entries++
		s.Levels[level]++
	}
	sc.mu.Unlock()
}

// Results returns a snapshot of all per-source stats.
func (sc *statsCollector) Results() map[string]SourceStats {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := make(map[string]SourceStats, len(sc.sources))
	for k, v := range sc.sources {
		cp := *v
		cp.Levels = make(map[types.Level]int64, len(v.Levels))
		for l, c := range v.Levels {
			cp.Levels[l] = c
		}
		out[k] = cp
	}
	return out
}

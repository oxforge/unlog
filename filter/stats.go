package filter

// workerStats holds per-worker counters. Not thread-safe —
// each worker owns its own instance.
type workerStats struct {
	ingested       int64
	passed         int64
	droppedByLevel int64
	droppedByTime  int64
	droppedByNoise int64
	// errorsBySource counts ERROR and FATAL occurrences that passed the
	// filters, before merging.
	errorsBySource map[string]int64
}

// mergedStats is the combination of all workers' counters.
type mergedStats struct {
	ingested       int64
	passed         int64
	errorsBySource map[string]int64
	detailed       DetailedStats
}

// mergeWorkerStats combines per-worker stats. DroppedByDedup is left for
// phase 2, which knows how many records survived.
func mergeWorkerStats(workers []workerStats) mergedStats {
	m := mergedStats{errorsBySource: make(map[string]int64)}
	for _, w := range workers {
		m.ingested += w.ingested
		m.passed += w.passed
		m.detailed.DroppedByLevel += w.droppedByLevel
		m.detailed.DroppedByTimeWindow += w.droppedByTime
		m.detailed.DroppedByNoise += w.droppedByNoise
		for src, n := range w.errorsBySource {
			m.errorsBySource[src] += n
		}
	}
	return m
}

package filter

// workerStats holds per-worker drop counters. Not thread-safe —
// each worker owns its own instance.
type workerStats struct {
	ingested       int64
	droppedByLevel int64
	droppedByTime  int64
	droppedByNoise int64
	droppedByDedup int64
}

// mergeWorkerStats combines per-worker stats into DetailedStats.
func mergeWorkerStats(workers []workerStats) (ingested int64, detailed DetailedStats) {
	for _, w := range workers {
		ingested += w.ingested
		detailed.DroppedByLevel += w.droppedByLevel
		detailed.DroppedByTimeWindow += w.droppedByTime
		detailed.DroppedByNoise += w.droppedByNoise
		detailed.DroppedByDedup += w.droppedByDedup
	}
	return ingested, detailed
}

package filter

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/oxforge/unlog/types"
	"golang.org/x/sync/errgroup"
)

// FilterOptions configures the filter pipeline behavior.
type FilterOptions struct {
	// MinLevel is the minimum log level to keep. Default: LevelWarn.
	MinLevel types.Level
	// Since filters entries before this time. Zero means no filter.
	Since time.Time
	// Until filters entries after this time. Zero means no filter.
	Until time.Time
	// Workers is the number of parallel filter goroutines. Default: runtime.NumCPU().
	Workers int
	// DedupShards is the number of shards for the dedup records. Default: 16.
	DedupShards int
	// NoiseFile is the path to a custom noise patterns file. Empty = use built-in only.
	NoiseFile string
	// SpikeMultiplier sets how far above its source's normal rate a second
	// must be to count toward a burst: more than SpikeMultiplier × the median
	// per-second count, and at least 2 events. Default: 10.
	SpikeMultiplier float64
	// MinBurstEvents is the fewest events a burst must hold. Default: 20.
	MinBurstEvents int
}

// DefaultFilterOptions returns FilterOptions with all defaults set.
func DefaultFilterOptions() FilterOptions {
	return FilterOptions{
		MinLevel:        types.LevelWarn,
		Workers:         runtime.NumCPU(),
		DedupShards:     16,
		SpikeMultiplier: 10,
		MinBurstEvents:  20,
	}
}

// withDefaults returns a copy of opts with zero values replaced by defaults.
func withDefaults(opts FilterOptions) FilterOptions {
	defaults := DefaultFilterOptions()
	if opts.MinLevel == 0 {
		opts.MinLevel = defaults.MinLevel
	}
	if opts.Workers == 0 {
		opts.Workers = defaults.Workers
	}
	if opts.DedupShards == 0 {
		opts.DedupShards = defaults.DedupShards
	}
	if opts.SpikeMultiplier == 0 {
		opts.SpikeMultiplier = defaults.SpikeMultiplier
	}
	if opts.MinBurstEvents == 0 {
		opts.MinBurstEvents = defaults.MinBurstEvents
	}
	return opts
}

// DetailedStats holds per-filter drop counts for diagnostics.
type DetailedStats struct {
	DroppedByLevel      int64
	DroppedByTimeWindow int64
	DroppedByNoise      int64
	DroppedByDedup      int64
	BurstCount          int64
}

// FilterPipeline connects stages 1 and 3 via streaming filters.
type FilterPipeline struct {
	input   <-chan types.LogEntry
	output  chan<- types.FilteredEntry
	opts    FilterOptions
	filters []EntryFilter
	// dedup is the dedup filter, tracked separately for flush at end.
	dedup  *DedupFilter
	bursts []types.Burst
}

// Bursts returns the bursts found by Run. It is set before any entry is emitted,
// so it is safe to read once the output channel is closed.
func (p *FilterPipeline) Bursts() []types.Burst {
	return p.bursts
}

// NewFilterPipeline creates a new FilterPipeline with defaults applied.
func NewFilterPipeline(
	input <-chan types.LogEntry,
	output chan<- types.FilteredEntry,
	opts FilterOptions,
	filters []EntryFilter,
) *FilterPipeline {
	opts = withDefaults(opts)
	return &FilterPipeline{
		input:   input,
		output:  output,
		opts:    opts,
		filters: filters,
	}
}

// Run executes the filter pipeline. It closes the output channel when done.
// Returns FilterStats, DetailedStats, and any error encountered.
func (p *FilterPipeline) Run(ctx context.Context) (types.FilterStats, DetailedStats, error) {
	defer close(p.output)
	start := time.Now()

	var chain []EntryFilter
	chain = append(chain, NewLevelFilter(p.opts.MinLevel))

	tw := NewTimeWindowFilter(p.opts.Since, p.opts.Until)
	if tw.IsActive() {
		chain = append(chain, tw)
	}

	nf, err := NewNoiseFilter(p.opts.NoiseFile)
	if err != nil {
		return types.FilterStats{}, DetailedStats{}, fmt.Errorf("filter: noise: %w", err)
	}
	chain = append(chain, nf)

	chain = append(chain, p.filters...)

	dedup := NewDedupFilter(p.opts.DedupShards)
	p.dedup = dedup

	// Phase 1: Fan-out workers.
	allWorkerStats := make([]workerStats, p.opts.Workers)
	for i := range allWorkerStats {
		allWorkerStats[i].errorsBySource = make(map[string]int64)
	}
	allNotes := make([][]occurrence, p.opts.Workers)

	g, gctx := errgroup.WithContext(ctx)

	for w := 0; w < p.opts.Workers; w++ {
		g.Go(func() error {
			ws := &allWorkerStats[w]

			for {
				select {
				case <-gctx.Done():
					return gctx.Err()
				case entry, ok := <-p.input:
					if !ok {
						return nil
					}

					ws.ingested++

					dropped := false
					for _, f := range chain {
						if !f.Filter(entry) {
							switch f.Name() {
							case "level":
								ws.droppedByLevel++
							case "timewindow":
								ws.droppedByTime++
							case "noise":
								ws.droppedByNoise++
							}
							dropped = true
							break
						}
					}
					if dropped {
						continue
					}

					ws.passed++
					if entry.Level == types.LevelError || entry.Level == types.LevelFatal {
						ws.errorsBySource[entry.Source]++
					}
					id := dedup.Add(entry)
					if !entry.Timestamp.IsZero() {
						allNotes[w] = append(allNotes[w], occurrence{record: id, second: entry.Timestamp.Unix()})
					}
				}
			}
		})
	}

	if err := g.Wait(); err != nil && err != context.Canceled {
		return types.FilterStats{}, DetailedStats{}, fmt.Errorf("filter: %w", err)
	}

	totals := mergeWorkerStats(allWorkerStats)
	detailed := totals.detailed

	var notes []occurrence
	for _, n := range allNotes {
		notes = append(notes, n...)
	}
	return p.postProcess(ctx, dedup, notes, totals, &detailed, start)
}

// postProcess runs phase 2 (burst detection), emits one entry per record in
// sorted order, and computes final stats.
func (p *FilterPipeline) postProcess(
	ctx context.Context,
	dedup *DedupFilter,
	notes []occurrence,
	totals mergedStats,
	detailed *DetailedStats,
	start time.Time,
) (types.FilterStats, DetailedStats, error) {
	p.bursts = detectBursts(notes, dedup, p.opts.SpikeMultiplier, int64(p.opts.MinBurstEvents))
	detailed.BurstCount = int64(len(p.bursts))
	records := dedup.Entries()

	for i := range records {
		select {
		case p.output <- records[i]:
		case <-ctx.Done():
			return types.FilterStats{}, DetailedStats{}, ctx.Err()
		}
	}

	survived := int64(len(records))
	detailed.DroppedByDedup = totals.passed - survived

	// The window covers dated records only.
	var winStart, winEnd time.Time
	for i := range records {
		if records[i].FirstSeen.IsZero() {
			continue
		}
		if winStart.IsZero() || records[i].FirstSeen.Before(winStart) {
			winStart = records[i].FirstSeen
		}
		if records[i].LastSeen.After(winEnd) {
			winEnd = records[i].LastSeen
		}
	}

	fs := types.FilterStats{
		TotalIngested:    totals.ingested,
		TotalDropped:     totals.ingested - survived,
		TotalSurvived:    survived,
		UniqueSignatures: dedup.UniqueSignatures(),
		TimeWindowStart:  winStart,
		TimeWindowEnd:    winEnd,
		SourceBreakdown:  totals.errorsBySource,
		FilterDurationMs: time.Since(start).Milliseconds(),
	}

	return fs, *detailed, nil
}

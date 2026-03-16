package filter

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
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
	// AutoWindow enables automatic interesting-window detection. Default: true.
	AutoWindow bool
	// AutoWindowPadding is the time to add around detected interesting periods. Default: 15m.
	AutoWindowPadding time.Duration
	// Workers is the number of parallel filter goroutines. Default: runtime.NumCPU().
	Workers int
	// MaxDuplicates is the max times an exact duplicate is kept. Default: 5.
	MaxDuplicates int
	// DedupShards is the number of shards for the dedup cache. Default: 16.
	DedupShards int
	// FuzzyDedupCacheSize is the LRU size for fuzzy signatures. Default: 10_000.
	FuzzyDedupCacheSize int
	// MaxSurvivors is the maximum number of entries that survive filtering. Default: 1_000_000.
	MaxSurvivors int
	// NoiseFile is the path to a custom noise patterns file. Empty = use built-in only.
	NoiseFile string
	// SpikeMultiplier is the rate increase factor to consider a spike. Default: 10.
	SpikeMultiplier float64
	// IsStdin indicates that input is from stdin (disables features requiring re-read).
	IsStdin bool
}

// DefaultFilterOptions returns FilterOptions with all defaults set.
func DefaultFilterOptions() FilterOptions {
	return FilterOptions{
		MinLevel:            types.LevelWarn,
		AutoWindow:          true,
		AutoWindowPadding:   15 * time.Minute,
		Workers:             runtime.NumCPU(),
		MaxDuplicates:       5,
		DedupShards:         16,
		FuzzyDedupCacheSize: 10_000,
		MaxSurvivors:        1_000_000,
		SpikeMultiplier:     10,
	}
}

// withDefaults returns a copy of opts with zero values replaced by defaults.
func withDefaults(opts FilterOptions) FilterOptions {
	defaults := DefaultFilterOptions()
	if opts.MinLevel == 0 {
		opts.MinLevel = defaults.MinLevel
	}
	if opts.AutoWindowPadding == 0 {
		opts.AutoWindowPadding = defaults.AutoWindowPadding
	}
	if opts.Workers == 0 {
		opts.Workers = defaults.Workers
	}
	if opts.MaxDuplicates == 0 {
		opts.MaxDuplicates = defaults.MaxDuplicates
	}
	if opts.DedupShards == 0 {
		opts.DedupShards = defaults.DedupShards
	}
	if opts.FuzzyDedupCacheSize == 0 {
		opts.FuzzyDedupCacheSize = defaults.FuzzyDedupCacheSize
	}
	if opts.MaxSurvivors == 0 {
		opts.MaxSurvivors = defaults.MaxSurvivors
	}
	if opts.SpikeMultiplier == 0 {
		opts.SpikeMultiplier = defaults.SpikeMultiplier
	}
	return opts
}

// DetailedStats holds per-filter drop counts for diagnostics.
type DetailedStats struct {
	DroppedByLevel      int64
	DroppedByTimeWindow int64
	DroppedByNoise      int64
	DroppedByDedup      int64
	DroppedByAutoWindow int64
	SpikeCount          int64
}

// FilterPipeline connects stages 1 and 3 via streaming filters.
type FilterPipeline struct {
	input   <-chan types.LogEntry
	output  chan<- types.FilteredEntry
	opts    FilterOptions
	filters []EntryFilter
	// dedup is the dedup filter, tracked separately for flush at end.
	dedup *DedupFilter
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

	dedup := NewDedupFilter(p.opts.MaxDuplicates, p.opts.DedupShards, p.opts.FuzzyDedupCacheSize)
	p.dedup = dedup

	// Phase 1: Fan-out workers.
	var survivorCount atomic.Int64
	var overflowEmitted atomic.Int64
	var overflow atomic.Bool

	allWorkerStats := make([]workerStats, p.opts.Workers)
	allSurvivors := make([][]types.FilteredEntry, p.opts.Workers)

	g, gctx := errgroup.WithContext(ctx)

	for w := 0; w < p.opts.Workers; w++ {
		w := w
		g.Go(func() error {
			ws := &allWorkerStats[w]
			var locals []types.FilteredEntry

			for {
				select {
				case <-gctx.Done():
					allSurvivors[w] = locals
					return gctx.Err()
				case entry, ok := <-p.input:
					if !ok {
						allSurvivors[w] = locals
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

					fe, kept := dedup.Apply(entry)
					if !kept {
						ws.droppedByDedup++
						continue
					}

					if overflow.Load() {
						// Emit directly to output, skip local buffering.
						select {
						case p.output <- fe:
							overflowEmitted.Add(1)
						case <-gctx.Done():
							allSurvivors[w] = locals
							return gctx.Err()
						}
						continue
					}

					cur := survivorCount.Add(1)
					if cur > int64(p.opts.MaxSurvivors) {
						overflow.Store(true)
						// Emit directly.
						select {
						case p.output <- fe:
							overflowEmitted.Add(1)
						case <-gctx.Done():
							allSurvivors[w] = locals
							return gctx.Err()
						}
						continue
					}

					locals = append(locals, fe)
				}
			}
		})
	}

	if err := g.Wait(); err != nil && err != context.Canceled {
		return types.FilterStats{}, DetailedStats{}, fmt.Errorf("filter: %w", err)
	}

	ingested, detailed := mergeWorkerStats(allWorkerStats)

	var totalLen int
	for _, s := range allSurvivors {
		totalLen += len(s)
	}
	survivors := make([]types.FilteredEntry, 0, totalLen)
	for _, s := range allSurvivors {
		survivors = append(survivors, s...)
	}

	// Phase 2 (skipped if overflow).
	if !overflow.Load() {
		// Spike detection.
		detailed.SpikeCount = DetectSpikes(survivors, p.opts.SpikeMultiplier)

		// Auto-window.
		if p.opts.AutoWindow && !p.opts.IsStdin && p.opts.Since.IsZero() && p.opts.Until.IsZero() {
			var awDropped int64
			survivors, awDropped = DetectAutoWindow(survivors, p.opts.AutoWindowPadding)
			detailed.DroppedByAutoWindow = awDropped
		}

		sort.Slice(survivors, func(i, j int) bool {
			return survivors[i].Timestamp.Before(survivors[j].Timestamp)
		})
	}

	for i := range survivors {
		select {
		case p.output <- survivors[i]:
		case <-ctx.Done():
			return types.FilterStats{}, DetailedStats{}, ctx.Err()
		}
	}

	summaries := dedup.Summaries()
	for i := range summaries {
		select {
		case p.output <- summaries[i]:
		case <-ctx.Done():
			return types.FilterStats{}, DetailedStats{}, ctx.Err()
		}
	}

	survived := int64(len(survivors)) + overflowEmitted.Load()
	dropped := ingested - survived

	sourceBreakdown := make(map[string]int64)
	for i := range survivors {
		if survivors[i].Level == types.LevelError || survivors[i].Level == types.LevelFatal {
			sourceBreakdown[survivors[i].Source]++
		}
	}

	var winStart, winEnd time.Time
	if len(survivors) > 0 {
		winStart = survivors[0].Timestamp
		winEnd = survivors[len(survivors)-1].Timestamp
	}

	autoDetected := detailed.DroppedByAutoWindow > 0

	fs := types.FilterStats{
		TotalIngested:      ingested,
		TotalDropped:       dropped,
		TotalSurvived:      survived,
		UniqueSignatures:   dedup.UniqueSignatures(),
		TimeWindowStart:    winStart,
		TimeWindowEnd:      winEnd,
		AutoDetectedWindow: autoDetected,
		SourceBreakdown:    sourceBreakdown,
		FilterDurationMs:   time.Since(start).Milliseconds(),
	}

	return fs, detailed, nil
}

// Package pipeline orchestrates stages 1-4 of the unlog pipeline.
// It wires ingest → filter → enrich → compact via buffered channels,
// using errgroup for goroutine lifecycle management.
package pipeline

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/oxforge/unlog/compact"
	"github.com/oxforge/unlog/enrich"
	"github.com/oxforge/unlog/filter"
	"github.com/oxforge/unlog/ingest"
	"github.com/oxforge/unlog/types"
)

const chanBuffer = 100_000

// Options configures the pipeline.
type Options struct {
	IngestOpts  ingest.IngestOptions
	FilterOpts  filter.FilterOptions
	EnrichOpts  enrich.EnrichOptions
	CompactOpts compact.Options
}

// Result holds everything the pipeline produces.
type Result struct {
	Summary       string
	Stats         types.FilterStats
	DetailedStats filter.DetailedStats
	Duration      time.Duration
}

type Pipeline struct {
	opts Options
}

func New(opts Options) *Pipeline {
	return &Pipeline{opts: opts}
}

// Run executes the pipeline against the given sources.
func (p *Pipeline) Run(ctx context.Context, sources []string) (*Result, error) {
	start := time.Now()

	ingestCh := make(chan types.LogEntry, chanBuffer)
	filterCh := make(chan types.FilteredEntry, chanBuffer)
	enrichCh := make(chan types.EnrichedEntry, chanBuffer)

	g, gCtx := errgroup.WithContext(ctx)

	ing := ingest.NewIngester(ingestCh, p.opts.IngestOpts)
	g.Go(func() error {
		if err := ing.Run(gCtx, sources); err != nil {
			return fmt.Errorf("pipeline: ingest: %w", err)
		}
		return nil
	})

	type filterOut struct {
		stats    types.FilterStats
		detailed filter.DetailedStats
	}
	filterOutCh := make(chan filterOut, 1)

	fp := filter.NewFilterPipeline(ingestCh, filterCh, p.opts.FilterOpts, nil)
	g.Go(func() error {
		fs, ds, err := fp.Run(gCtx)
		if err != nil {
			return fmt.Errorf("pipeline: filter: %w", err)
		}
		filterOutCh <- filterOut{fs, ds}
		return nil
	})

	ep := enrich.NewEnricher(filterCh, enrichCh, p.opts.EnrichOpts)
	g.Go(func() error {
		if err := ep.Run(gCtx); err != nil {
			return fmt.Errorf("pipeline: enrich: %w", err)
		}
		return nil
	})

	compactOutCh := make(chan string, 1)
	g.Go(func() error {
		s, err := compact.Run(gCtx, enrichCh, p.opts.CompactOpts)
		// Always send the result (possibly empty) so the read below never
		// blocks. On error g.Wait() returns early and the value is unused.
		compactOutCh <- s
		if err != nil {
			return fmt.Errorf("pipeline: compact: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	fo := <-filterOutCh
	return &Result{
		Summary:       <-compactOutCh,
		Stats:         fo.stats,
		DetailedStats: fo.detailed,
		Duration:      time.Since(start),
	}, nil
}

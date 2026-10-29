package enrich

import (
	"context"
	"time"

	"github.com/oxforge/unlog/types"
)

// EnrichOptions configures the enrichment pipeline.
type EnrichOptions struct {
	// CorrelationWindow is the time window for cross-source correlation. Default: 5s.
	CorrelationWindow time.Duration
}

// DefaultOptions returns EnrichOptions with sensible defaults.
func DefaultOptions() EnrichOptions {
	return EnrichOptions{
		CorrelationWindow: 5 * time.Second,
	}
}

// Enricher is the Stage 3 pipeline component.
type Enricher struct {
	input  <-chan types.FilteredEntry
	output chan<- types.EnrichedEntry

	fields *FieldExtractor
	deploy *DeployDetector
	chains *ChainMatcher
	corr   *Correlator
}

// NewEnricher creates an Enricher wired to the given channels.
func NewEnricher(input <-chan types.FilteredEntry, output chan<- types.EnrichedEntry, opts EnrichOptions) *Enricher {
	return &Enricher{
		input:  input,
		output: output,
		fields: NewFieldExtractor(),
		deploy: NewDeployDetector(),
		chains: NewChainMatcher(),
		corr:   NewCorrelator(opts.CorrelationWindow),
	}
}

// Run processes all entries and closes the output channel when done.
func (e *Enricher) Run(ctx context.Context) error {
	defer close(e.output)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry, ok := <-e.input:
			if !ok {
				return nil
			}

			enriched := types.EnrichedEntry{
				FilteredEntry: entry,
			}

			e.fields.Extract(&enriched)
			e.deploy.Detect(&enriched)
			enriched.ChainID = e.chains.Match(&enriched)
			e.corr.Correlate(&enriched)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case e.output <- enriched:
			}
		}
	}
}

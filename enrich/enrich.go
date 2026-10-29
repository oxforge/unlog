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

// NewEnricher creates an Enricher wired to the given channels. Zero option
// fields take their defaults.
func NewEnricher(input <-chan types.FilteredEntry, output chan<- types.EnrichedEntry, opts EnrichOptions) *Enricher {
	if opts.CorrelationWindow <= 0 {
		opts.CorrelationWindow = DefaultOptions().CorrelationWindow
	}
	return &Enricher{
		input:  input,
		output: output,
		fields: NewFieldExtractor(),
		deploy: NewDeployDetector(),
		chains: NewChainMatcher(),
		corr:   NewCorrelator(opts.CorrelationWindow),
	}
}

// Run enriches all entries and closes the output channel when done. It
// reads every entry first (the filter stage emits only after reading all
// input anyway), because a chain is known only once its later stages are
// seen, and then tags every member. Entries keep their input order.
func (e *Enricher) Run(ctx context.Context) error {
	defer close(e.output)

	var entries []types.EnrichedEntry
collect:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case entry, ok := <-e.input:
			if !ok {
				break collect
			}
			enriched := types.EnrichedEntry{FilteredEntry: entry}
			e.fields.Extract(&enriched)
			e.deploy.Detect(&enriched)
			entries = append(entries, enriched)
		}
	}

	for _, c := range e.chains.FindChains(entries) {
		for _, m := range c.Members {
			member := &entries[m.Index]
			member.Chains = append(member.Chains, types.ChainLink{ID: c.ID, Step: m.Step, Steps: c.Steps})
			if member.ChainID == "" {
				member.ChainID, member.ChainStep, member.ChainSteps = c.ID, m.Step, c.Steps
			}
		}
	}

	e.corr.Correlate(entries)
	for i := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e.output <- entries[i]:
		}
	}
	return nil
}

// Package compact implements Stage 4 of the unlog pipeline: priority scoring
// and token-budgeted compaction. It reads EnrichedEntry values, scores them by
// importance, and produces a structured text summary that fits within an LLM
// context window.
//
// The output is organized into six sections, each with a base share of the
// token budget; what a section does not need goes to the sections that would
// otherwise be cut, in the order of this list (they print overview first,
// then deployments, then the rest):
//
//	Incident Overview  5% (fixed)
//	Critical Errors   45%
//	Error Chains      20%
//	Deployments        5%
//	Rate Anomalies    10%
//	Context           15%
//
// A section keeps its highest-scored entries and lists them in time order.
//
// This package is public — it can be imported by the enterprise repo.
package compact

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/oxforge/unlog/types"
)

const (
	// DefaultTokenBudget is the default maximum token count for the compacted
	// summary. 8 192 fits comfortably in the context window of every major LLM
	// while leaving room for system prompts and the response.
	DefaultTokenBudget = 8_192
)

// Options configures the compact stage.
type Options struct {
	// TokenBudget is the maximum estimated token count for the summary output.
	// Default: DefaultTokenBudget.
	TokenBudget int
	// Bursts returns the bursts to list under Rate Anomalies. Run calls it
	// once, after the entries channel is closed and only when ctx is not
	// cancelled; nil means none.
	Bursts func() []types.Burst
}

// withDefaults returns o with zero-value fields replaced by defaults.
func (o Options) withDefaults() Options {
	if o.TokenBudget <= 0 {
		o.TokenBudget = DefaultTokenBudget
	}
	return o
}

// scoredEntry pairs an EnrichedEntry with its computed priority score.
type scoredEntry struct {
	entry types.EnrichedEntry
	score int
}

// Run consumes all EnrichedEntry values from entries, scores them, and returns
// a structured text summary within the token budget. Every entry is kept: the
// filter stage has already applied the minimum level, and entries whose level
// is unknown land in Context with the lowest score. Blocks until entries is
// closed or ctx is cancelled.
func Run(ctx context.Context, entries <-chan types.EnrichedEntry, opts Options) (string, error) {
	opts = opts.withDefaults()

	var all []scoredEntry
drain:
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("compact: %w", ctx.Err())
		case entry, ok := <-entries:
			if !ok {
				break drain
			}
			all = append(all, scoredEntry{
				entry: entry,
				score: Score(entry),
			})
		}
	}
	slog.Debug("compact: collected entries", "count", len(all))

	// On cancellation an upstream stage closes the input early, possibly
	// before the filter has finished its bursts: stop before reading them.
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("compact: %w", err)
	}
	var bursts []types.Burst
	if opts.Bursts != nil {
		bursts = opts.Bursts()
	}

	if len(all) == 0 {
		return buildEmptySummary(), nil
	}

	// A total order, so equal entries never swap when unrelated scores change.
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].entry, all[j].entry
		switch {
		case all[i].score != all[j].score:
			return all[i].score > all[j].score
		case !a.Timestamp.Equal(b.Timestamp):
			return a.Timestamp.Before(b.Timestamp)
		case a.Source != b.Source:
			return a.Source < b.Source
		case a.LineNumber != b.LineNumber:
			return a.LineNumber < b.LineNumber
		default:
			return a.Message < b.Message
		}
	})

	summary := formatSummary(all, bursts, opts.TokenBudget)

	actual := EstimateTokens(summary)
	slog.Debug("compact: summary generated",
		"entries_in", len(all),
		"estimated_tokens", actual,
		"budget", opts.TokenBudget)

	return summary, nil
}

// buildEmptySummary returns a minimal summary when no entries are provided.
func buildEmptySummary() string {
	return "## Incident Overview\nNo significant log entries found.\n\n" +
		"## Critical Errors\n(none)\n\n" +
		"## Error Chains\n(none)\n\n" +
		"## Rate Anomalies\n(none)\n\n" +
		"## Context\n(none)"
}

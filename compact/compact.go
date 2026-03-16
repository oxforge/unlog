// Package compact implements Stage 4 of the unlog pipeline: priority scoring
// and token-budgeted compaction. It reads EnrichedEntry values, scores them by
// importance, and produces a structured text summary that fits within an LLM
// context window.
//
// The output is organized into five sections with fixed token-budget fractions:
//
//	Incident Overview  5%
//	Critical Errors   45%
//	Error Chains      20%
//	Rate Anomalies    10%
//	Context           20%
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

	// DefaultChannelBuffer is the default input channel buffer size.
	DefaultChannelBuffer = 100_000
)

// Options configures the compact stage.
type Options struct {
	// TokenBudget is the maximum estimated token count for the summary output.
	// Default: DefaultTokenBudget.
	TokenBudget int
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
// a structured text summary within the token budget. Entries below LevelInfo
// are silently dropped. Blocks until entries is closed or ctx is cancelled.
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
			if entry.Level < types.LevelInfo {
				continue
			}
			all = append(all, scoredEntry{
				entry: entry,
				score: Score(entry),
			})
		}
	}
	slog.Debug("compact: collected entries", "count", len(all))

	if len(all) == 0 {
		return buildEmptySummary(), nil
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].entry.Timestamp.Before(all[j].entry.Timestamp)
	})

	summary := formatSummary(all, opts.TokenBudget)

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

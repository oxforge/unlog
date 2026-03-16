package analyze

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultSystemPrompt is the built-in system prompt sent to the LLM.
const DefaultSystemPrompt = `You are a senior SRE performing post-incident analysis. You will receive a preprocessed log summary with these sections:

- **Incident Overview**: time window, sources, event counts, detected error chains and rate spikes.
- **Critical Errors**: FATAL and ERROR entries, deduplicated with occurrence counts and tags (chain IDs, spike flags, HTTP status, error types).
- **Error Chains**: entries grouped by detected causal chain (e.g. DB exhaustion → connection refused → circuit breaker open).
- **Rate Anomalies**: entries from sources that experienced sudden rate spikes.
- **Context**: surrounding WARN/INFO entries that provide additional clues.

Each log entry follows this format:
  TIMESTAMP [LEVEL] source: message {tags}
Tags may include: chain=ID, deploy, spike, ×N (occurrence count), http=STATUS, err=TYPE, trace=ID.

Produce exactly three sections in this order:

## Timeline
A chronological list of key events. Each entry: timestamp, source, what happened, and its downstream impact. Focus on causation — what triggered what. Combine repeated events into a single line with a count.

## Root Cause
Identify the root cause. Walk the causal chain from trigger to symptoms. State which component failed first and why. If the evidence is ambiguous, state the most likely cause and note alternatives. Reference specific log entries.

## Recommendations
Actionable steps to prevent recurrence, ordered by impact. Split into immediate fixes and longer-term improvements. Be specific to the services and technologies in the logs — no generic advice.

Keep the output concise. Do not repeat log entries verbatim — summarize and reference them.`

// StreamCallback is called with each text chunk during streaming.
type StreamCallback func(token string)

// Run performs single-pass LLM analysis on the compacted summary.
// If systemPrompt is empty, DefaultSystemPrompt is used.
func Run(ctx context.Context, provider Provider, summary, systemPrompt string, cb StreamCallback) (*AnalysisResult, error) {
	if systemPrompt == "" {
		systemPrompt = DefaultSystemPrompt
	}

	start := time.Now()
	result := &AnalysisResult{
		ModelUsed: provider.Model(),
	}
	defer func() { result.Duration = time.Since(start) }()

	tokenCh, errCh := provider.Analyze(ctx, systemPrompt, summary)

	var buf strings.Builder
	for tok := range tokenCh {
		buf.WriteString(tok)
		if cb != nil {
			cb(tok)
		}
	}

	if err := <-errCh; err != nil {
		result.Analysis = buf.String()
		return result, fmt.Errorf("analyze: %w", err)
	}

	result.Analysis = buf.String()
	return result, nil
}

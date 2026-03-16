package analyze

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const systemPrompt = "You are an expert SRE analyzing a log incident. Given the following compacted log summary, produce: 1) A chronological incident timeline, 2) Root cause analysis, 3) Recommendations to prevent recurrence. Be concise and actionable."

// StreamCallback is called with each text chunk during streaming.
type StreamCallback func(token string)

// Run performs single-pass LLM analysis on the compacted summary.
func Run(ctx context.Context, provider Provider, summary string, cb StreamCallback) (*AnalysisResult, error) {
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

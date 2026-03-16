package analyze

import (
	"context"
	"fmt"
	"strings"
)

const (
	timelinePrompt = "You are an expert SRE analyzing a log incident. Given the following compacted log summary, produce a chronological timeline of the incident. Each entry should have a timestamp (or relative time), what happened, and which service/component was affected. Focus on causation — what triggered what."

	rootCausePrompt = "You are an expert SRE performing root cause analysis. Given the log summary and the incident timeline below, identify the root cause. Explain the chain of causation from the trigger to the symptoms. Be specific about which component failed first and why."

	recommendationsPrompt = "You are an expert SRE writing remediation recommendations. Given the log summary, timeline, and root cause analysis below, provide actionable recommendations to prevent recurrence. Prioritize by impact. Include both immediate fixes and longer-term improvements."

	fastPrompt = "You are an expert SRE analyzing a log incident. Given the following compacted log summary, produce: 1) A chronological incident timeline, 2) Root cause analysis, 3) Recommendations to prevent recurrence. Be concise and actionable."
)

// Options configures the analysis run.
type Options struct {
	Fast bool // single-pass mode (--fast)
}

// StreamCallback is called with each text chunk during streaming.
type StreamCallback func(pass Pass, token string)

// Run performs LLM analysis on the compacted summary. Multi-pass by default
// (timeline -> root cause -> recommendations); single pass with opts.Fast.
func Run(ctx context.Context, provider Provider, summary string, opts Options, cb StreamCallback) (*AnalysisResult, error) {
	result := &AnalysisResult{
		ModelUsed: provider.Model(),
	}

	if opts.Fast {
		output, err := runPass(ctx, provider, fastPrompt, summary, PassTimeline, cb)
		if err != nil {
			result.Timeline = output
			return result, fmt.Errorf("analyze: fast pass: %w", err)
		}
		result.Timeline = output
		result.RootCause = output
		result.Recommendations = output
		return result, nil
	}

	timeline, err := runPass(ctx, provider, timelinePrompt, summary, PassTimeline, cb)
	result.Timeline = timeline
	if err != nil {
		return result, fmt.Errorf("analyze: timeline pass: %w", err)
	}

	rootCauseInput := buildChainedPrompt(summary, timeline, "")
	rootCause, err := runPass(ctx, provider, rootCausePrompt, rootCauseInput, PassRootCause, cb)
	result.RootCause = rootCause
	if err != nil {
		return result, fmt.Errorf("analyze: root cause pass: %w", err)
	}

	recsInput := buildChainedPrompt(summary, timeline, rootCause)
	recs, err := runPass(ctx, provider, recommendationsPrompt, recsInput, PassRecommendations, cb)
	result.Recommendations = recs
	if err != nil {
		return result, fmt.Errorf("analyze: recommendations pass: %w", err)
	}

	return result, nil
}

// runPass executes a single LLM pass and returns the accumulated text.
func runPass(ctx context.Context, provider Provider, system, prompt string, pass Pass, cb StreamCallback) (string, error) {
	tokenCh, errCh := provider.Analyze(ctx, system, prompt)

	var buf strings.Builder
	for tok := range tokenCh {
		buf.WriteString(tok)
		if cb != nil {
			cb(pass, tok)
		}
	}

	if err := <-errCh; err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// buildChainedPrompt assembles the user prompt for multi-pass analysis.
func buildChainedPrompt(summary, timeline, rootCause string) string {
	var buf strings.Builder
	buf.WriteString("## Log Summary\n\n")
	buf.WriteString(summary)
	buf.WriteString("\n\n## Incident Timeline\n\n")
	buf.WriteString(timeline)
	if rootCause != "" {
		buf.WriteString("\n\n## Root Cause Analysis\n\n")
		buf.WriteString(rootCause)
	}
	return buf.String()
}

// Package analyze implements Stage 5 of the unlog pipeline: optional
// LLM-powered incident analysis. This package is internal — it is a
// leaf dependency, never imported by other packages.
package analyze

import (
	"context"
	"time"
)

// Provider sends a prompt to an LLM and streams back text chunks.
type Provider interface {
	// Analyze streams LLM response chunks. Callers must drain both channels.
	Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error)
	Name() string
	Model() string
}

// AnalysisResult holds the three-section output from LLM analysis.
type AnalysisResult struct {
	Timeline        string
	RootCause       string
	Recommendations string
	ModelUsed       string
	Duration        time.Duration
}

// Pass identifies which analysis pass is running.
type Pass int

const (
	PassTimeline Pass = iota
	PassRootCause
	PassRecommendations
)

func (p Pass) String() string {
	switch p {
	case PassTimeline:
		return "timeline"
	case PassRootCause:
		return "root_cause"
	case PassRecommendations:
		return "recommendations"
	default:
		return "unknown"
	}
}

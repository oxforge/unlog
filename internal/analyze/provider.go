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

// AnalysisResult holds the single-pass output from LLM analysis.
type AnalysisResult struct {
	Analysis  string
	ModelUsed string
	Duration  time.Duration
}

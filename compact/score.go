package compact

import (
	"github.com/oxforge/unlog/types"
)

// scoreWeights defines the contribution of each signal to the priority score.
// All values are additive; the final score is unbounded but relative.
const (
	// Level weights — higher severity dominates.
	weightFatal = 100
	weightError = 60
	weightWarn  = 20
	weightInfo  = 5

	// Contextual signal weights.
	weightSpike      = 40 // rate spike detected by filter stage
	weightChain      = 50 // entry participates in a known error chain
	weightDeployment = 30 // entry is a deployment/restart event

	// Occurrence weight: log(occurrenceCount) * this factor.
	// Capped at occurrenceCap to prevent one noisy pattern dominating.
	weightOccurrencePerLog = 5
	occurrenceCap          = 20
)

// Score computes a priority score for a single EnrichedEntry.
// Higher score = more important = kept first under token budget pressure.
func Score(e types.EnrichedEntry) int {
	score := levelScore(e.Level)

	if e.IsSpike {
		score += weightSpike
	}
	if e.ChainID != "" {
		score += weightChain
	}
	if e.IsDeployment {
		score += weightDeployment
	}

	// Occurrence bonus: repeated events are more significant up to a cap.
	occ := e.OccurrenceCount
	if occ > occurrenceCap {
		occ = occurrenceCap
	}
	if occ > 1 {
		score += ilog2(occ) * weightOccurrencePerLog
	}

	return score
}

// levelScore maps a Level to a base priority score.
func levelScore(l types.Level) int {
	switch l {
	case types.LevelFatal:
		return weightFatal
	case types.LevelError:
		return weightError
	case types.LevelWarn:
		return weightWarn
	case types.LevelInfo:
		return weightInfo
	default:
		return 0
	}
}

// ilog2 returns floor(log2(n)) for n >= 1. Returns 0 for n <= 0.
func ilog2(n int) int {
	if n <= 0 {
		return 0
	}
	result := 0
	for n > 1 {
		n >>= 1
		result++
	}
	return result
}

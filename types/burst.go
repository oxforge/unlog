package types

import "time"

// Burst is a run of seconds in which one source (and service, when its lines
// name one) logged far above its normal rate.
type Burst struct {
	Source    string       `json:"source"`
	Service   string       `json:"service,omitempty"`
	Start     time.Time    `json:"start"` // first second of the burst
	End       time.Time    `json:"end"`   // last second of the burst
	Events    int64        `json:"events"`
	Baseline  float64      `json:"baseline_per_sec"`
	TopErrors []BurstError `json:"top_errors"` // up to 3, most in-burst occurrences first
}

// BurstError is one error's share of a burst.
type BurstError struct {
	Message string `json:"message"`
	Level   Level  `json:"level"`
	Count   int64  `json:"count"` // occurrences inside the burst
}

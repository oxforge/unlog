package types

import "time"

// LogEntry is produced by Stage 1 (Ingest). One per log line.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     Level     `json:"level"`
	Source    string    `json:"source"`
	// Service is the service, app, container or pod the line names, when it
	// names one; Source stays the file. Errors and bursts are split per
	// (Source, Service).
	Service    string            `json:"service,omitempty"`
	Message    string            `json:"message"`
	RawLine    string            `json:"raw_line,omitempty"`
	LineNumber int64             `json:"line_number"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// FilteredEntry is produced by Stage 2 (Filter). Adds dedup info.
type FilteredEntry struct {
	LogEntry
	OccurrenceCount int `json:"occurrence_count"`
	// OccurrencesByLevel counts the occurrences at each level; Level is the
	// highest of them.
	OccurrencesByLevel map[Level]int `json:"occurrences_by_level,omitempty"`
	FirstSeen          time.Time     `json:"first_seen"`
	LastSeen           time.Time     `json:"last_seen"`
	IsSpike            bool          `json:"is_spike"`
	// Deprecated: never set; dedup emits one entry per error.
	IsDedupSummary bool   `json:"is_dedup_summary,omitempty"`
	Signature      string `json:"signature"`
}

// ChainLink places an entry in one detected error chain.
type ChainLink struct {
	ID    string `json:"id"`
	Step  int    `json:"step"`  // the stage (1-based) the entry matched
	Steps int    `json:"steps"` // the chain pattern's number of stages
}

// EnrichedEntry is produced by Stage 3 (Enrich). Adds structural context.
type EnrichedEntry struct {
	FilteredEntry
	// Chains lists every error chain the entry belongs to, in the order the
	// chains were found. ChainID, ChainStep and ChainSteps repeat the first.
	Chains  []ChainLink `json:"chains,omitempty"`
	ChainID string      `json:"chain_id,omitempty"`
	// ChainStep is the stage (1-based) of the chain this entry matched, and
	// ChainSteps the chain pattern's number of stages; both 0 outside a chain.
	ChainStep    int  `json:"chain_step,omitempty"`
	ChainSteps   int  `json:"chain_steps,omitempty"`
	IsDeployment bool `json:"is_deployment"`
	// CorrelatedWith names the first few (by name) of the other origins whose
	// errors started within the correlation window of this one, and
	// CorrelatedCount how many there are in all. Both are set on dated
	// ERROR and FATAL entries only.
	CorrelatedWith  []string `json:"correlated_with,omitempty"`
	CorrelatedCount int      `json:"correlated_count,omitempty"`
	ErrorType       string   `json:"error_type,omitempty"`
	HTTPStatus      int      `json:"http_status,omitempty"`
	TraceID         string   `json:"trace_id,omitempty"`
}

// ChainLinks returns the chains the entry belongs to: Chains, or the single
// chain ChainID names when only that is set.
func (e EnrichedEntry) ChainLinks() []ChainLink {
	if len(e.Chains) == 0 && e.ChainID != "" {
		return []ChainLink{{ID: e.ChainID, Step: e.ChainStep, Steps: e.ChainSteps}}
	}
	return e.Chains
}

// Origin labels where the entry came from: the source, plus the service in
// brackets when the line names one ("agg.log[payments]").
func (e LogEntry) Origin() string {
	if e.Service == "" {
		return e.Source
	}
	return e.Source + "[" + e.Service + "]"
}

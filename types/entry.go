package types

import "time"

// LogEntry is produced by Stage 1 (Ingest). One per log line.
type LogEntry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Level      Level             `json:"level"`
	Source     string            `json:"source"`
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

// EnrichedEntry is produced by Stage 3 (Enrich). Adds structural context.
type EnrichedEntry struct {
	FilteredEntry
	ChainID        string   `json:"chain_id,omitempty"`
	IsDeployment   bool     `json:"is_deployment"`
	CorrelatedWith []string `json:"correlated_with,omitempty"`
	ErrorType      string   `json:"error_type,omitempty"`
	HTTPStatus     int      `json:"http_status,omitempty"`
	TraceID        string   `json:"trace_id,omitempty"`
}

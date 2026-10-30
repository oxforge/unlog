package types

import "time"

// AnalysisReport is the final output of the full pipeline.
type AnalysisReport struct {
	GeneratedAt  time.Time `json:"generated_at"`
	UnlogVersion string    `json:"unlog_version"`
	// Deprecated: unlog no longer calls LLMs (v0.4.0); always empty and left out of the JSON.
	ModelUsed          string `json:"model_used,omitempty"`
	AnalysisDurationMs int64  `json:"analysis_duration_ms"`
	// Deprecated: unlog no longer calls LLMs (v0.4.0); always empty and left out of the JSON.
	AIDurationMs int64       `json:"ai_duration_ms,omitempty"`
	Stats        FilterStats `json:"stats"`
	// Deprecated: unlog no longer calls LLMs (v0.4.0); always empty and left out of the JSON.
	Analysis string `json:"analysis,omitempty"`
	// Deprecated: unlog no longer calls LLMs (v0.4.0); always empty and left out of the JSON.
	AnalysisError    string `json:"analysis_error,omitempty"`
	CompactedSummary string `json:"compacted_summary"`
}

// FilterStats tracks what the filter stage did.
type FilterStats struct {
	TotalIngested    int64     `json:"ingested"`
	TotalDropped     int64     `json:"dropped"`
	TotalSurvived    int64     `json:"survived"`
	UniqueSignatures int       `json:"unique_signatures"`
	TimeWindowStart  time.Time `json:"time_window_start"`
	TimeWindowEnd    time.Time `json:"time_window_end"`
	// Deprecated: always false; unlog no longer narrows the time range on its
	// own. Kept for report consumers.
	AutoDetectedWindow bool             `json:"auto_detected_window"`
	SourceBreakdown    map[string]int64 `json:"source_breakdown"`
	FileCount          int              `json:"file_count"`
	BytesProcessed     int64            `json:"bytes_processed"`
	FilterDurationMs   int64            `json:"duration_ms"`
}

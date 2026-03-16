package types

import "time"

// AnalysisReport is the final output of the full pipeline.
type AnalysisReport struct {
	GeneratedAt      time.Time     `json:"generated_at"`
	UnlogVersion     string        `json:"unlog_version"`
	ModelUsed        string        `json:"model_used,omitempty"`
	AnalysisDuration time.Duration `json:"analysis_duration"`
	Stats            FilterStats   `json:"stats"`
	Timeline         string        `json:"timeline,omitempty"`
	RootCause        string        `json:"root_cause,omitempty"`
	Recommendations  string        `json:"recommendations,omitempty"`
	CompactedSummary string        `json:"compacted_summary"`
}

// FilterStats tracks what the filter stage did.
type FilterStats struct {
	TotalIngested      int64            `json:"total_ingested"`
	TotalDropped       int64            `json:"total_dropped"`
	TotalSurvived      int64            `json:"total_survived"`
	UniqueSignatures   int              `json:"unique_signatures"`
	TimeWindowStart    time.Time        `json:"time_window_start"`
	TimeWindowEnd      time.Time        `json:"time_window_end"`
	AutoDetectedWindow bool             `json:"auto_detected_window"`
	SourceBreakdown    map[string]int64 `json:"source_breakdown,omitempty"`
	FileCount          int              `json:"file_count"`
	BytesProcessed     int64            `json:"bytes_processed"`
	FilterDuration     time.Duration    `json:"filter_duration"`
}

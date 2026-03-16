package enrich

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/oxforge/unlog/types"
)

// Compiled regexes for field extraction, initialized once.
var (
	reHTTPStatus = regexp.MustCompile(`(?:HTTP[/ ][\d.]+\s+|status[= :]?\s*)(\d{3})\b`)
	reErrorType  = regexp.MustCompile(`(?:(?:Caused by|Exception|Error|Panic|FATAL):\s*)([\w.]+)`)
	reTraceID    = regexp.MustCompile(`(?i)(?:trace[_-]?id|request[_-]?id|correlation[_-]?id)[=: ]+["']?([a-zA-Z0-9\-]{8,})`)
)

// httpStatusKeys are metadata keys checked for HTTP status codes.
var httpStatusKeys = []string{"status", "status_code", "http_status", "statusCode", "response_code"}

// errorTypeKeys are metadata keys checked for error type names.
var errorTypeKeys = []string{"error", "error.type", "exception", "exception.type", "err"}

// traceIDKeys are metadata keys checked for trace/request IDs.
var traceIDKeys = []string{"trace_id", "traceId", "trace", "request_id", "requestId", "x-request-id", "correlation_id"}

// FieldExtractor extracts structured fields (HTTPStatus, ErrorType, TraceID)
// from log entries using metadata-first lookup with regex fallback.
type FieldExtractor struct{}

// NewFieldExtractor creates a new FieldExtractor.
func NewFieldExtractor() *FieldExtractor {
	return &FieldExtractor{}
}

// Extract populates HTTPStatus, ErrorType, and TraceID on the given entry.
func (f *FieldExtractor) Extract(entry *types.EnrichedEntry) {
	f.extractHTTPStatus(entry)
	f.extractErrorType(entry)
	f.extractTraceID(entry)
}

func (f *FieldExtractor) extractHTTPStatus(entry *types.EnrichedEntry) {
	if entry.Metadata != nil {
		for _, key := range httpStatusKeys {
			if v, ok := entry.Metadata[key]; ok {
				if code, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && code >= 100 && code <= 599 {
					entry.HTTPStatus = code
					return
				}
			}
		}
	}
	if m := reHTTPStatus.FindStringSubmatch(entry.Message); len(m) > 1 {
		if code, err := strconv.Atoi(m[1]); err == nil && code >= 100 && code <= 599 {
			entry.HTTPStatus = code
		}
	}
}

func (f *FieldExtractor) extractErrorType(entry *types.EnrichedEntry) {
	if entry.Metadata != nil {
		for _, key := range errorTypeKeys {
			if v, ok := entry.Metadata[key]; ok && v != "" {
				entry.ErrorType = v
				return
			}
		}
	}
	if m := reErrorType.FindStringSubmatch(entry.Message); len(m) > 1 {
		entry.ErrorType = m[1]
	}
}

func (f *FieldExtractor) extractTraceID(entry *types.EnrichedEntry) {
	if entry.Metadata != nil {
		for _, key := range traceIDKeys {
			if v, ok := entry.Metadata[key]; ok && v != "" {
				entry.TraceID = v
				return
			}
		}
	}
	if m := reTraceID.FindStringSubmatch(entry.Message); len(m) > 1 {
		entry.TraceID = m[1]
	}
}

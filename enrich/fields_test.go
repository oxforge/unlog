package enrich

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func makeEnrichedEntry(msg string, meta map[string]string) types.EnrichedEntry {
	return types.EnrichedEntry{
		FilteredEntry: types.FilteredEntry{
			LogEntry: types.LogEntry{
				Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
				Level:     types.LevelError,
				Source:    "test-svc",
				Message:   msg,
				Metadata:  meta,
			},
		},
	}
}

func TestFieldExtractor_HTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		metadata map[string]string
		want     int
	}{
		{"metadata status", "", map[string]string{"status": "503"}, 503},
		{"metadata status_code", "", map[string]string{"status_code": "404"}, 404},
		{"metadata http_status", "", map[string]string{"http_status": "200"}, 200},
		{"metadata statusCode", "", map[string]string{"statusCode": "500"}, 500},
		{"metadata response_code", "", map[string]string{"response_code": "502"}, 502},
		{"regex HTTP/1.1 500", "HTTP/1.1 500 Internal Server Error", nil, 500},
		{"regex status=503", "request failed status=503", nil, 503},
		{"regex status: 404", "response status: 404", nil, 404},
		{"no match", "everything is fine", nil, 0},
		{"metadata takes priority", "HTTP/1.1 500", map[string]string{"status": "503"}, 503},
		{"out of range 999", "status=999", nil, 0},
		{"out of range 000", "status=000", nil, 0},
	}

	fe := NewFieldExtractor()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := makeEnrichedEntry(tt.message, tt.metadata)
			fe.Extract(&entry)
			assert.Equal(t, tt.want, entry.HTTPStatus)
		})
	}
}

func TestFieldExtractor_ErrorType(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		metadata map[string]string
		want     string
	}{
		{"metadata error", "", map[string]string{"error": "ConnectionRefused"}, "ConnectionRefused"},
		{"metadata error.type", "", map[string]string{"error.type": "TimeoutError"}, "TimeoutError"},
		{"metadata exception", "", map[string]string{"exception": "NullPointerException"}, "NullPointerException"},
		{"regex Exception:", "java.lang.NullPointerException", nil, ""},
		{"regex Exception with colon", "Exception: java.lang.NullPointerException", nil, "java.lang.NullPointerException"},
		{"regex Caused by", "Caused by: com.example.CustomError", nil, "com.example.CustomError"},
		{"regex Error:", "Error: ECONNREFUSED", nil, "ECONNREFUSED"},
		{"regex Panic:", "Panic: runtime.gopanic", nil, "runtime.gopanic"},
		{"no match", "connection closed", nil, ""},
		{"empty message", "", nil, ""},
		{"metadata priority", "Error: Fallback", map[string]string{"error.type": "Primary"}, "Primary"},
	}

	fe := NewFieldExtractor()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := makeEnrichedEntry(tt.message, tt.metadata)
			fe.Extract(&entry)
			assert.Equal(t, tt.want, entry.ErrorType)
		})
	}
}

func TestFieldExtractor_TraceID(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		metadata map[string]string
		want     string
	}{
		{"metadata trace_id", "", map[string]string{"trace_id": "abc-123-def-456"}, "abc-123-def-456"},
		{"metadata traceId", "", map[string]string{"traceId": "abc12345"}, "abc12345"},
		{"metadata request_id", "", map[string]string{"request_id": "req-12345678"}, "req-12345678"},
		{"metadata x-request-id", "", map[string]string{"x-request-id": "xreq-abcd1234"}, "xreq-abcd1234"},
		{"metadata correlation_id", "", map[string]string{"correlation_id": "corr-99887766"}, "corr-99887766"},
		{"regex trace_id=", "processing trace_id=abc12345def request", nil, "abc12345def"},
		{"regex request-id:", "request-id: req-12345678-abcd", nil, "req-12345678-abcd"},
		{"no match", "no trace here", nil, ""},
		{"metadata priority", "trace_id=fallback", map[string]string{"trace_id": "primary-id"}, "primary-id"},
	}

	fe := NewFieldExtractor()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := makeEnrichedEntry(tt.message, tt.metadata)
			fe.Extract(&entry)
			assert.Equal(t, tt.want, entry.TraceID)
		})
	}
}

func TestFieldExtractor_EmptyMessage(t *testing.T) {
	fe := NewFieldExtractor()
	entry := makeEnrichedEntry("", map[string]string{"status": "500", "error": "Timeout", "trace_id": "abc12345"})
	fe.Extract(&entry)
	assert.Equal(t, 500, entry.HTTPStatus)
	assert.Equal(t, "Timeout", entry.ErrorType)
	assert.Equal(t, "abc12345", entry.TraceID)
}

func TestFieldExtractor_NilMetadata(t *testing.T) {
	fe := NewFieldExtractor()
	entry := types.EnrichedEntry{
		FilteredEntry: types.FilteredEntry{
			LogEntry: types.LogEntry{
				Message:  "HTTP/1.1 500 Error: Timeout trace_id=abc12345",
				Metadata: nil,
			},
		},
	}
	fe.Extract(&entry)
	assert.Equal(t, 500, entry.HTTPStatus)
	assert.Equal(t, "Timeout", entry.ErrorType)
	assert.Equal(t, "abc12345", entry.TraceID)
}

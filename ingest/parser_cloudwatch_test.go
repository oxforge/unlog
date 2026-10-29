package ingest

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestCloudWatchParser(t *testing.T) {
	p := &cloudwatchParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"standard cloudwatch",
			`{"@timestamp":"2024-01-15T10:00:00.000Z","@message":"ERROR: connection refused","@logStream":"app/prod/abc123"}`,
			true, types.LevelError, "ERROR: connection refused",
		},
		{
			"info message",
			`{"@timestamp":"2024-01-15T10:00:00.000Z","@message":"Starting server","@logStream":"app/prod/abc123"}`,
			true, types.LevelUnknown, "Starting server",
		},
		{
			"not cloudwatch",
			`{"level":"info","msg":"test"}`,
			false, types.LevelUnknown, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantMsg, entry.Message)
				// Timestamp assertions
				assert.Equal(t, 2024, entry.Timestamp.Year())
				assert.Equal(t, 10, entry.Timestamp.Hour())
				// Metadata assertions
				assert.Equal(t, "app/prod/abc123", entry.Metadata["logStream"])
			}
		})
	}
}

func TestCloudWatchParserEpochMillisTimestamp(t *testing.T) {
	p := &cloudwatchParser{}
	entry, ok := p.Parse(`{"@timestamp":1705312801250,"@message":"Task timed out after 30.00 seconds"}`, 1, "lambda.log")
	assert.True(t, ok)
	want := time.Date(2024, 1, 15, 10, 0, 1, 250000000, time.UTC)
	assert.True(t, want.Equal(entry.Timestamp), "want %s, got %s", want, entry.Timestamp)
}

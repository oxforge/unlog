package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestInferLevel(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want types.Level
	}{
		{"bare keyword", "ERROR something", types.LevelError},
		{"trailing colon", "ERROR: connection refused", types.LevelError},
		{"brackets", "[ERROR] thing", types.LevelError},
		{"token 2", "10:30:00 WARN disk full", types.LevelWarn},
		{"token 3", "2024-01-15 10:30:00 FATAL OOM", types.LevelFatal},
		{"no keyword", "connection pool exhausted", types.LevelUnknown},
		{"empty", "", types.LevelUnknown},
		{"case insensitive", "error: foo", types.LevelError},
		{"info level", "INFO application started", types.LevelInfo},
		{"debug level", "[DEBUG] loading config", types.LevelDebug},
		{"warn alias", "WARNING: high memory", types.LevelWarn},
		{"keyword beyond 5 tokens", "one two three four five FATAL crash", types.LevelUnknown},
		{"numeric token skipped", "Retrying in 5 seconds", types.LevelUnknown},
		{"numeric syslog severity skipped", "attempt 2 failed", types.LevelUnknown},
		{"decimal skipped", "latency 3.5 ERROR timeout", types.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferLevel(tt.msg)
			assert.Equal(t, tt.want, got, "InferLevel(%q)", tt.msg)
		})
	}
}

func TestIngesterCSVWithoutLevelColumn(t *testing.T) {
	// The CSV header regex needs timestamp,level,message columns. Data rows
	// with a level value match the CSV data regex and win majority vote for
	// format detection. Rows with empty level get LevelUnknown from the CSV
	// parser, then InferLevel fills in from the message body.
	csv := `timestamp,level,message,source
2024-01-15T10:00:00Z,INFO,healthy check passed,api
2024-01-15T10:00:01Z,WARN,disk usage high,api
2024-01-15T10:00:02Z,ERROR,connection timeout,api
2024-01-15T10:00:03Z,INFO,request completed,api
2024-01-15T10:00:04Z,DEBUG,cache hit,api
2024-01-15T10:00:05Z,,ERROR: Connection pool exhausted,api
2024-01-15T10:00:10Z,,WARN: Slow query detected,api
2024-01-15T10:00:15Z,,Application started successfully,api
`
	output := make(chan types.LogEntry, 100)
	ingester := NewIngester(output, IngestOptions{SampleLines: 100})

	err := ingester.processSource(context.Background(), "test.csv", strings.NewReader(csv))
	close(output)
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}

	assert.Len(t, entries, 8)
	assert.Equal(t, types.LevelInfo, entries[0].Level)    // explicit level column
	assert.Equal(t, types.LevelWarn, entries[1].Level)    // explicit level column
	assert.Equal(t, types.LevelError, entries[2].Level)   // explicit level column
	assert.Equal(t, types.LevelInfo, entries[3].Level)    // explicit level column
	assert.Equal(t, types.LevelDebug, entries[4].Level)   // explicit level column
	assert.Equal(t, types.LevelError, entries[5].Level)   // inferred from message
	assert.Equal(t, types.LevelWarn, entries[6].Level)    // inferred from message
	assert.Equal(t, types.LevelUnknown, entries[7].Level) // no keyword in message
}

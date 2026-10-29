package ingest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/oxforge/unlog/types"
)

// Container runtimes wrap the application's own log line; these tests check
// that the wrapped line is read, not just the wrapper.
func TestContainerParsersReadStructuredPayloads(t *testing.T) {
	tests := []struct {
		name      string
		parser    Parser
		line      string
		wantLevel types.Level
		wantMsg   string
		wantTS    time.Time
		wantMeta  map[string]string
	}{
		{
			name:      "docker stderr with zap JSON",
			parser:    &dockerParser{},
			line:      `{"log":"{\"level\":\"info\",\"ts\":1705312801.5,\"msg\":\"cache warmed\",\"keys\":42}\n","stream":"stderr","time":"2024-01-15T10:00:02Z"}`,
			wantLevel: types.LevelInfo,
			wantMsg:   "cache warmed",
			wantTS:    time.Date(2024, 1, 15, 10, 0, 1, 500000000, time.UTC),
			wantMeta:  map[string]string{"stream": "stderr", "keys": "42"},
		},
		{
			name:      "docker stdout with logfmt",
			parser:    &dockerParser{},
			line:      `{"log":"level=error msg=\"db down\" db=pg\n","stream":"stdout","time":"2024-01-15T10:00:02Z"}`,
			wantLevel: types.LevelError,
			wantMsg:   "db down",
			wantTS:    time.Date(2024, 1, 15, 10, 0, 2, 0, time.UTC),
			wantMeta:  map[string]string{"stream": "stdout", "db": "pg"},
		},
		{
			name:      "CRI stderr with JSON",
			parser:    &kubeParser{},
			line:      `2024-01-15T10:00:02.000000000Z stderr F {"level":"error","msg":"reconcile failed","controller":"deploy"}`,
			wantLevel: types.LevelError,
			wantMsg:   "reconcile failed",
			wantTS:    time.Date(2024, 1, 15, 10, 0, 2, 0, time.UTC),
			wantMeta:  map[string]string{"stream": "stderr", "controller": "deploy"},
		},
		{
			name:      "compose with JSON",
			parser:    &dockerComposeParser{},
			line:      `api-1  | {"level":"warn","msg":"slow query","ms":812,"time":"2024-01-15T10:00:02Z"}`,
			wantLevel: types.LevelWarn,
			wantMsg:   "slow query",
			wantTS:    time.Date(2024, 1, 15, 10, 0, 2, 0, time.UTC),
			wantMeta:  map[string]string{"container": "api-1", "ms": "812"},
		},
		{
			name:      "cloudwatch with JSON",
			parser:    &cloudwatchParser{},
			line:      `{"@timestamp":"2024-01-15T10:00:02.000Z","@message":"{\"level\":\"error\",\"message\":\"batch write failed\",\"table\":\"orders\"}"}`,
			wantLevel: types.LevelError,
			wantMsg:   "batch write failed",
			wantTS:    time.Date(2024, 1, 15, 10, 0, 2, 0, time.UTC),
			wantMeta:  map[string]string{"table": "orders"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := tt.parser.Parse(tt.line, 1, "c.log")
			assert.True(t, ok)
			assert.Equal(t, tt.wantLevel, entry.Level)
			assert.Equal(t, tt.wantMsg, entry.Message)
			assert.True(t, tt.wantTS.Equal(entry.Timestamp), "want %s, got %s", tt.wantTS, entry.Timestamp)
			assert.Equal(t, tt.wantMeta, entry.Metadata)
		})
	}
}

func TestContainerStreamIsOnlyAWeakHint(t *testing.T) {
	tests := []struct {
		name string
		line string
		want types.Level
	}{
		{"stderr plain text has no level",
			`{"log":"2024/01/15 10:00:00 server listening on :8080\n","stream":"stderr","time":"2024-01-15T10:00:00Z"}`, types.LevelUnknown},
		{"stdout plain text is info",
			`{"log":"server listening on :8080\n","stream":"stdout","time":"2024-01-15T10:00:00Z"}`, types.LevelInfo},
		{"level after a bracketed timestamp on stdout",
			`{"log":"[2024-01-15 10:00:05] WARN: deprecated API used\n","stream":"stdout","time":"2024-01-15T10:00:05Z"}`, types.LevelWarn},
		{"level after a bracketed timestamp on stderr",
			`{"log":"[2024-01-15 10:00:11] ERROR: database connection lost\n","stream":"stderr","time":"2024-01-15T10:00:11Z"}`, types.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := (&dockerParser{}).Parse(tt.line, 1, "c.log")
			assert.True(t, ok)
			assert.Equal(t, tt.want, entry.Level)
		})
	}
}

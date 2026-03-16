package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestJSONParser(t *testing.T) {
	p := &jsonParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"standard fields",
			`{"level":"error","msg":"connection failed","timestamp":"2024-01-15T10:00:00Z","host":"db-1"}`,
			true, types.LevelError, "connection failed",
		},
		{
			"severity field",
			`{"severity":"WARNING","message":"high latency","ts":"2024-01-15T10:00:00Z"}`,
			true, types.LevelWarn, "high latency",
		},
		{
			"time field",
			`{"level":"info","msg":"started","time":"2024-01-15T10:00:00Z"}`,
			true, types.LevelInfo, "started",
		},
		{
			"no level field",
			`{"msg":"something happened","ts":"2024-01-15T10:00:00Z"}`,
			true, types.LevelUnknown, "something happened",
		},
		{
			"invalid json",
			`not json at all`,
			false, types.LevelUnknown, "",
		},
		{
			"empty object",
			`{}`,
			true, types.LevelUnknown, "",
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
				switch tt.name {
				case "standard fields":
					assert.Equal(t, 2024, entry.Timestamp.Year())
					assert.Equal(t, 10, entry.Timestamp.Hour())
					assert.Equal(t, "db-1", entry.Metadata["host"])
				case "severity field", "time field", "no level field":
					assert.Equal(t, 2024, entry.Timestamp.Year())
					assert.Equal(t, 10, entry.Timestamp.Hour())
				}
			}
		})
	}
}

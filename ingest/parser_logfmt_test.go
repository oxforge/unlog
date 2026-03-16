package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestLogfmtParser(t *testing.T) {
	p := &logfmtParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"standard logfmt",
			`ts=2024-01-15T10:00:00Z level=error msg="connection failed" host=db-1 port=5432`,
			true, types.LevelError, "connection failed",
		},
		{
			"unquoted message",
			`ts=2024-01-15T10:00:00Z level=info msg=starting`,
			true, types.LevelInfo, "starting",
		},
		{
			"time key",
			`time=2024-01-15T10:00:00Z level=warn msg="high latency" duration=1.5s`,
			true, types.LevelWarn, "high latency",
		},
		{
			"no level",
			`ts=2024-01-15T10:00:00Z msg="something happened"`,
			true, types.LevelUnknown, "something happened",
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
				switch tt.name {
				case "standard logfmt":
					assert.Equal(t, "db-1", entry.Metadata["host"])
					assert.Equal(t, "5432", entry.Metadata["port"])
				case "time key":
					assert.Equal(t, "1.5s", entry.Metadata["duration"])
				}
			}
		})
	}
}

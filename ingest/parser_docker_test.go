package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestDockerParser(t *testing.T) {
	p := &dockerParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"stdout",
			`{"log":"Starting server on port 8080\n","stream":"stdout","time":"2024-01-15T10:00:00.000000000Z"}`,
			true, types.LevelInfo, "Starting server on port 8080",
		},
		{
			"stderr",
			`{"log":"ERROR: connection refused\n","stream":"stderr","time":"2024-01-15T10:00:01.000000000Z"}`,
			true, types.LevelError, "ERROR: connection refused",
		},
		{
			"not docker json",
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
				assert.Contains(t, entry.Metadata, "stream")
				switch tt.name {
				case "stdout":
					assert.Equal(t, "stdout", entry.Metadata["stream"])
				case "stderr":
					assert.Equal(t, "stderr", entry.Metadata["stream"])
				}
			}
		})
	}
}

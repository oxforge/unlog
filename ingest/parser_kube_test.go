package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestKubeParser(t *testing.T) {
	p := &kubeParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"stdout full line",
			`2024-01-15T10:00:00.000000000Z stdout F Starting application`,
			true, types.LevelInfo, "Starting application",
		},
		{
			"stderr error",
			`2024-01-15T10:00:01.000000000Z stderr F ERROR: connection refused`,
			true, types.LevelError, "ERROR: connection refused",
		},
		{
			"partial line",
			`2024-01-15T10:00:02.000000000Z stdout P partial content`,
			true, types.LevelInfo, "partial content",
		},
		{
			"not kube format",
			`just some text`,
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
			}
		})
	}
}

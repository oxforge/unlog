package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestGenericParser(t *testing.T) {
	p := &genericParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"ISO timestamp with level",
			"2024-01-15 10:00:00 ERROR connection refused",
			true, types.LevelError, "ERROR connection refused",
		},
		{
			"ISO T-separated",
			"2024-01-15T10:00:00 INFO starting server",
			true, types.LevelInfo, "INFO starting server",
		},
		{
			"no level keyword",
			"2024-01-15 10:00:00 something happened",
			true, types.LevelUnknown, "something happened",
		},
		{
			"no timestamp",
			"just some text",
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

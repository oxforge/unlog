package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestRawParser(t *testing.T) {
	p := &rawParser{}
	tests := []struct {
		name    string
		line    string
		wantMsg string
	}{
		{"plain text", "some log line", "some log line"},
		{"empty line", "", ""},
		{"with numbers", "error code 42", "error code 42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.True(t, ok)
			assert.Equal(t, tt.wantMsg, entry.Message)
			assert.Equal(t, types.LevelUnknown, entry.Level)
			assert.True(t, entry.Timestamp.IsZero())
		})
	}
}

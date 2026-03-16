package filter

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestLevelFilter(t *testing.T) {
	f := NewLevelFilter(types.LevelWarn)

	tests := []struct {
		name   string
		level  types.Level
		expect bool
	}{
		{"fatal kept", types.LevelFatal, true},
		{"error kept", types.LevelError, true},
		{"warn kept", types.LevelWarn, true},
		{"info dropped", types.LevelInfo, false},
		{"debug dropped", types.LevelDebug, false},
		{"trace dropped", types.LevelTrace, false},
		{"unknown kept", types.LevelUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := types.LogEntry{Level: tt.level}
			assert.Equal(t, tt.expect, f.Filter(entry))
		})
	}

	t.Run("Name returns level", func(t *testing.T) {
		assert.Equal(t, "level", f.Name())
	})
}

package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestCLFParser(t *testing.T) {
	p := &clfParser{}
	tests := []struct {
		name       string
		line       string
		wantOK     bool
		wantLevel  types.Level
		wantStatus string
	}{
		{
			"200 OK",
			`192.168.1.1 - - [15/Jan/2024:10:00:00 -0700] "GET /api/users HTTP/1.1" 200 1234`,
			true, types.LevelInfo, "200",
		},
		{
			"404 Not Found",
			`10.0.0.1 - frank [15/Jan/2024:10:00:01 -0700] "GET /missing HTTP/1.1" 404 56`,
			true, types.LevelWarn, "404",
		},
		{
			"500 Server Error",
			`10.0.0.1 - - [15/Jan/2024:10:00:02 -0700] "POST /api/submit HTTP/1.1" 500 789`,
			true, types.LevelError, "500",
		},
		{
			"not CLF",
			`random log line`,
			false, types.LevelUnknown, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantStatus, entry.Metadata["status"])
				// Timestamp assertions (times are converted to UTC; 10:00 -0700 = 17:00 UTC)
				assert.Equal(t, 2024, entry.Timestamp.Year())
				assert.False(t, entry.Timestamp.IsZero())
				assert.Equal(t, 15, entry.Timestamp.Day())
			}
		})
	}
}

package ingest

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestSyslogParser3164(t *testing.T) {
	p := newSyslogParser(false)
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"standard 3164",
			`<34>Jan 15 10:00:00 myhost myapp[1234]: Connection refused`,
			true, types.LevelFatal, "Connection refused",
		},
		{
			"3164 no pid",
			`<134>Jan 15 10:00:00 myhost myapp: Starting up`,
			true, types.LevelInfo, "Starting up",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantMsg, entry.Message)
				// Timestamp: RFC 3164 has no year, so year is inferred as current year
				assert.Equal(t, time.Now().Year(), entry.Timestamp.Year())
				assert.Equal(t, time.January, entry.Timestamp.Month())
				assert.Equal(t, 15, entry.Timestamp.Day())
				assert.Equal(t, 10, entry.Timestamp.Hour())
				// Metadata assertions
				assert.Equal(t, "myhost", entry.Metadata["hostname"])
				assert.Equal(t, "myapp", entry.Metadata["app"])
				if tt.name == "standard 3164" {
					assert.Equal(t, "1234", entry.Metadata["pid"])
				}
			}
		})
	}
}

func TestSyslogParser5424(t *testing.T) {
	p := newSyslogParser(true)
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"standard 5424",
			`<165>1 2024-01-15T10:00:00.000Z myhost myapp 1234 - - Connection established`,
			true, types.LevelWarn, "Connection established",
		},
		{
			"5424 with structured data",
			`<134>1 2024-01-15T10:00:00Z host app 999 ID47 [exampleSDID@32473 iut="3"] Starting`,
			true, types.LevelInfo, "Starting",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantMsg, entry.Message)
				// Timestamp assertions for RFC 5424
				assert.Equal(t, 2024, entry.Timestamp.Year())
				assert.Equal(t, 10, entry.Timestamp.Hour())
				// Metadata assertions
				switch tt.name {
				case "standard 5424":
					assert.Equal(t, "myhost", entry.Metadata["hostname"])
					assert.Equal(t, "myapp", entry.Metadata["app"])
				case "5424 with structured data":
					assert.Equal(t, "host", entry.Metadata["hostname"])
					assert.Equal(t, "app", entry.Metadata["app"])
				}
			}
		})
	}
}

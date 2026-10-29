package ingest

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestSyslogParser3164(t *testing.T) {
	p := newSyslogParser(false, time.Now())
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
	p := newSyslogParser(true, time.Now())
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

func TestSyslogParserFileFormatWithoutPriority(t *testing.T) {
	ref := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	p := newSyslogParser(false, ref)

	entry, ok := p.Parse(`Jan 15 10:29:55 db-01 postgres[1234]: ERROR:  max_connections limit reached: 50/50`, 1, "db.log")

	assert.True(t, ok)
	assert.True(t, time.Date(2026, 1, 15, 10, 29, 55, 0, time.UTC).Equal(entry.Timestamp), "got %s", entry.Timestamp)
	assert.Equal(t, types.LevelUnknown, entry.Level, "no PRI, so the level is left to inference")
	assert.Equal(t, "ERROR:  max_connections limit reached: 50/50", entry.Message)
	assert.Equal(t, map[string]string{"hostname": "db-01", "app": "postgres", "pid": "1234"}, entry.Metadata)
}

func TestSyslogParserRsyslogISOFormat(t *testing.T) {
	p := newSyslogParser(false, time.Now())

	entry, ok := p.Parse(`2024-04-30T12:35:02.104233+02:00 web-1 nginx[812]: worker process 815 started`, 1, "syslog")

	assert.True(t, ok)
	assert.True(t, time.Date(2024, 4, 30, 10, 35, 2, 104233000, time.UTC).Equal(entry.Timestamp), "got %s", entry.Timestamp)
	assert.Equal(t, "worker process 815 started", entry.Message)
	assert.Equal(t, map[string]string{"hostname": "web-1", "app": "nginx", "pid": "812"}, entry.Metadata)
}

func TestSyslogParserYearFromReference(t *testing.T) {
	tests := []struct {
		name string
		ref  time.Time
		line string
		want time.Time
	}{
		{"same year", time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
			`Jan 15 10:00:00 host app: x`, time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)},
		{"December read in January is last year", time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC),
			`Dec 31 23:59:00 host app: x`, time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)},
		{"January read in January is this year", time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC),
			`Jan  1 00:00:01 host app: x`, time.Date(2027, 1, 1, 0, 0, 1, 0, time.UTC)},
		{"within a day of the reference is this year", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			`<34>Mar  1 12:00:00 host app: x`, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := newSyslogParser(false, tt.ref).Parse(tt.line, 1, "syslog")
			assert.True(t, ok)
			assert.True(t, tt.want.Equal(entry.Timestamp), "want %s, got %s", tt.want, entry.Timestamp)
		})
	}
}

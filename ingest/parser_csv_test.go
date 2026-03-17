package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestCSVParserWithHeader(t *testing.T) {
	p := &csvParser{}

	// First call with header should return false (skip).
	_, ok := p.Parse("timestamp,level,message,service,host", 1, "test.csv")
	assert.False(t, ok, "header row should be skipped")

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
		wantMeta  map[string]string
	}{
		{
			"info entry",
			"2024-01-15T10:00:00Z,INFO,Application starting,api,server-1",
			true, types.LevelInfo, "Application starting",
			map[string]string{"service": "api", "host": "server-1"},
		},
		{
			"error entry",
			"2024-01-15T10:00:10Z,ERROR,Connection pool exhausted,api,server-1",
			true, types.LevelError, "Connection pool exhausted",
			map[string]string{"service": "api", "host": "server-1"},
		},
		{
			"warn entry",
			"2024-01-15T10:00:05Z,WARN,Slow query detected,api,server-1",
			true, types.LevelWarn, "Slow query detected",
			map[string]string{"service": "api", "host": "server-1"},
		},
		{
			"fatal entry",
			"2024-01-15T10:00:15Z,FATAL,Unable to connect,api,server-1",
			true, types.LevelFatal, "Unable to connect",
			map[string]string{"service": "api", "host": "server-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 2, "test.csv")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantMsg, entry.Message)
				assert.Equal(t, 2024, entry.Timestamp.Year())
				assert.Equal(t, 10, entry.Timestamp.Hour())
				for k, v := range tt.wantMeta {
					assert.Equal(t, v, entry.Metadata[k], "metadata key %s", k)
				}
			}
		})
	}
}

func TestCSVParserNoHeader(t *testing.T) {
	p := &csvParser{}

	// First line is data, not a header — parser should reject all lines.
	_, ok := p.Parse("2024-01-15T10:00:00Z,ERROR,Something broke,extra-field", 1, "test.csv")
	assert.False(t, ok, "should reject first line without header")

	// Subsequent lines should also be rejected.
	_, ok = p.Parse("2024-01-15T10:00:01Z,INFO,All good", 2, "test.csv")
	assert.False(t, ok, "should reject all lines when no header was found")
}

func TestCSVParserQuotedFields(t *testing.T) {
	p := &csvParser{}

	// Header with quoted fields.
	_, ok := p.Parse(`"timestamp","level","message","service"`, 1, "test.csv")
	assert.False(t, ok, "header row should be skipped")

	entry, ok := p.Parse(`"2024-01-15T10:00:00Z","ERROR","Connection failed, retrying","api"`, 2, "test.csv")
	assert.True(t, ok)
	assert.Equal(t, types.LevelError, entry.Level)
	assert.Equal(t, "Connection failed, retrying", entry.Message)
	assert.Equal(t, "api", entry.Metadata["service"])
}

func TestCSVParserInvalidLine(t *testing.T) {
	p := &csvParser{}

	// Single field — not enough columns.
	_, ok := p.Parse("just-one-field", 1, "test.csv")
	assert.False(t, ok)
}

func TestCSVParserMissingFields(t *testing.T) {
	p := &csvParser{}

	// Header with 4 columns but data with only 2.
	_, ok := p.Parse("timestamp,level,message,service", 1, "test.csv")
	assert.False(t, ok)

	entry, ok := p.Parse("2024-01-15T10:00:00Z,ERROR", 2, "test.csv")
	assert.True(t, ok)
	assert.Equal(t, types.LevelError, entry.Level)
	assert.Equal(t, "", entry.Message) // missing column
}

func TestCSVParserAlternateHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header string
		line   string
	}{
		{"time/severity/msg", "time,severity,msg", "2024-01-15T10:00:00Z,error,fail"},
		{"ts/lvl/log", "ts,lvl,log", "2024-01-15T10:00:00Z,warn,problem"},
		{"date/loglevel/message", "date,loglevel,message", "2024-01-15T10:00:00Z,info,ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &csvParser{}
			_, ok := p.Parse(tt.header, 1, "test.csv")
			assert.False(t, ok, "header should be skipped")

			entry, ok := p.Parse(tt.line, 2, "test.csv")
			assert.True(t, ok)
			assert.NotEqual(t, types.LevelUnknown, entry.Level)
			assert.NotEmpty(t, entry.Message)
			assert.Equal(t, 2024, entry.Timestamp.Year())
		})
	}
}

func TestIsHeaderRow(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   bool
	}{
		{"standard headers", []string{"timestamp", "level", "message"}, true},
		{"uppercase variants", []string{"TIME", "SEVERITY", "MSG"}, true},
		{"short aliases", []string{"ts", "lvl", "log", "host"}, true},
		{"data not header", []string{"2024-01-15T10:00:00Z", "ERROR", "fail"}, false},
		{"unknown names", []string{"foo", "bar"}, false},
		{"too few fields", []string{"timestamp"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isHeaderRow(tt.fields))
		})
	}
}

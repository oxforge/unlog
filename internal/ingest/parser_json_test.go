package ingest

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestJSONParser(t *testing.T) {
	p := &jsonParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"standard fields",
			`{"level":"error","msg":"connection failed","timestamp":"2024-01-15T10:00:00Z","host":"db-1"}`,
			true, types.LevelError, "connection failed",
		},
		{
			"severity field",
			`{"severity":"WARNING","message":"high latency","ts":"2024-01-15T10:00:00Z"}`,
			true, types.LevelWarn, "high latency",
		},
		{
			"time field",
			`{"level":"info","msg":"started","time":"2024-01-15T10:00:00Z"}`,
			true, types.LevelInfo, "started",
		},
		{
			"no level field",
			`{"msg":"something happened","ts":"2024-01-15T10:00:00Z"}`,
			true, types.LevelUnknown, "something happened",
		},
		{
			"invalid json",
			`not json at all`,
			false, types.LevelUnknown, "",
		},
		{
			"empty object",
			`{}`,
			true, types.LevelUnknown, "",
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
				switch tt.name {
				case "standard fields":
					assert.Equal(t, 2024, entry.Timestamp.Year())
					assert.Equal(t, 10, entry.Timestamp.Hour())
					assert.Equal(t, "db-1", entry.Metadata["host"])
				case "severity field", "time field", "no level field":
					assert.Equal(t, 2024, entry.Timestamp.Year())
					assert.Equal(t, 10, entry.Timestamp.Hour())
				}
			}
		})
	}
}

func TestJSONParserNumericTimestamps(t *testing.T) {
	p := &jsonParser{}
	tests := []struct {
		name string
		line string
		want time.Time
	}{
		{"epoch seconds", `{"level":"error","ts":1705312801,"msg":"x"}`,
			time.Date(2024, 1, 15, 10, 0, 1, 0, time.UTC)},
		{"zap fractional epoch seconds", `{"level":"error","ts":1705312801.123456,"msg":"x"}`,
			time.Date(2024, 1, 15, 10, 0, 1, 123456000, time.UTC)},
		{"pino epoch millis", `{"level":50,"time":1705312801250,"msg":"x"}`,
			time.Date(2024, 1, 15, 10, 0, 1, 250000000, time.UTC)},
		{"epoch micros", `{"ts":1705312801000250,"msg":"x"}`,
			time.Date(2024, 1, 15, 10, 0, 1, 250000, time.UTC)},
		{"fractional epoch as string", `{"ts":"1705312801.5","msg":"x"}`,
			time.Date(2024, 1, 15, 10, 0, 1, 500000000, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.True(t, ok)
			assert.True(t, tt.want.Equal(entry.Timestamp), "want %s, got %s", tt.want, entry.Timestamp)
		})
	}
}

func TestJSONParserNumericLevels(t *testing.T) {
	p := &jsonParser{}
	tests := []struct {
		line string
		want types.Level
	}{
		{`{"level":10,"msg":"x"}`, types.LevelTrace},
		{`{"level":20,"msg":"x"}`, types.LevelDebug},
		{`{"level":30,"msg":"x"}`, types.LevelInfo},
		{`{"level":40,"msg":"x"}`, types.LevelWarn},
		{`{"level":50,"msg":"x"}`, types.LevelError},
		{`{"level":60,"msg":"x"}`, types.LevelFatal},
		{`{"level":3,"msg":"x"}`, types.LevelError}, // syslog severity
		{`{"level":7,"msg":"x"}`, types.LevelDebug}, // syslog severity
		{`{"level":9,"msg":"x"}`, types.LevelUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.True(t, ok)
			assert.Equal(t, tt.want, entry.Level)
		})
	}
}

func TestJSONParserKeepsLargeIntegersExact(t *testing.T) {
	p := &jsonParser{}
	entry, ok := p.Parse(`{"msg":"x","order_id":9007199254740993}`, 1, "test.log")
	assert.True(t, ok)
	assert.Equal(t, "9007199254740993", entry.Metadata["order_id"])
}

func TestJSONParserRejectsTrailingGarbage(t *testing.T) {
	p := &jsonParser{}
	_, ok := p.Parse(`{"msg":"x"} trailing`, 1, "test.log")
	assert.False(t, ok)
}

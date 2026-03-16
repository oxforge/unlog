package ingest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantYear int
		wantOK   bool
	}{
		{"RFC3339 full", "2024-01-15T10:30:45.123456789Z", 2024, true},
		{"RFC3339 offset", "2024-01-15T10:30:45.123+05:00", 2024, true},
		{"ISO8601 millis Z", "2024-01-15T10:30:45.123Z", 2024, true},
		{"ISO8601 no tz", "2024-01-15T10:30:45", 2024, true},
		{"Space separated millis", "2024-01-15 10:30:45.123", 2024, true},
		{"Space separated", "2024-01-15 10:30:45", 2024, true},
		{"Apache CLF", "15/Jan/2024:10:30:45 -0700", 2024, true},
		{"Syslog", "Jan 15 10:30:45", 0, true},
		{"Unix epoch seconds", "1705312245", 2024, true},
		{"Unix epoch millis", "1705312245000", 2024, true},
		{"Empty", "", 0, false},
		{"Garbage", "not a timestamp", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, _, ok := parseTimestamp(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK && tt.wantYear > 0 {
				assert.Equal(t, tt.wantYear, ts.Year())
			}
		})
	}
}

func TestTimestampFormatCache(t *testing.T) {
	cache := &formatCache{}

	ts1, ok := cache.Parse("2024-01-15T10:30:45.123Z")
	assert.True(t, ok)
	assert.Equal(t, 2024, ts1.Year())

	ts2, ok := cache.Parse("2024-01-15T11:00:00.456Z")
	assert.True(t, ok)
	assert.Equal(t, 11, ts2.Hour())

	ts3, ok := cache.Parse("2024-01-15 12:00:00")
	assert.True(t, ok)
	assert.Equal(t, 12, ts3.Hour())
}

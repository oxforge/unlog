package ingest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"Space separated comma millis", "2026-03-19 12:29:30,963", 2026, true},
		{"Space separated", "2024-01-15 10:30:45", 2024, true},
		{"Apache CLF", "15/Jan/2024:10:30:45 -0700", 2024, true},
		{"Syslog", "Jan 15 10:30:45", 0, true},
		{"Unix epoch seconds", "1705312245", 2024, true},
		{"Unix epoch millis", "1705312245000", 2024, true},
		{"Unix epoch micros", "1705312245000000", 2024, true},
		{"Unix epoch nanos", "1705312245000000000", 2024, true},
		{"Fractional epoch seconds", "1705312245.25", 2024, true},
		{"Too small for an epoch", "123456", 0, false},
		{"Signed fraction", "1705312245.+5", 0, false},
		{"Empty", "", 0, false},
		{"Garbage", "not a timestamp", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, _, ok := parseTimestamp(tt.input, time.UTC)
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

func TestFormatCacheZonelessTimestampsUseItsZone(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	inTokyo := formatCache{loc: tokyo}
	got, ok := inTokyo.Parse("2024-01-15 10:00:00")
	require.True(t, ok)
	assert.True(t, time.Date(2024, 1, 15, 1, 0, 0, 0, time.UTC).Equal(got), "got %s", got)

	withOffset, ok := inTokyo.Parse("2024-01-15T10:00:00Z")
	require.True(t, ok)
	assert.True(t, time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC).Equal(withOffset), "got %s", withOffset)

	var defaultZone formatCache
	got, ok = defaultZone.Parse("2024-01-15 10:00:00")
	require.True(t, ok)
	assert.True(t, time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC).Equal(got), "got %s", got)
}

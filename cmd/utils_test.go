package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTimeFlag(t *testing.T) {
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{"relative hours", "2h", time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)},
		{"relative minutes", "90m", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)},
		{"RFC 3339 in UTC", "2024-01-14T08:15:00Z", time.Date(2024, 1, 14, 8, 15, 0, 0, time.UTC)},
		{"RFC 3339 with offset", "2024-01-14T08:15:00+03:00", time.Date(2024, 1, 14, 5, 15, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimeFlag(tt.input, now)
			require.NoError(t, err)
			assert.True(t, tt.want.Equal(got), "want %s, got %s", tt.want, got)
		})
	}
}

func TestParseTimeFlagRejectsGarbage(t *testing.T) {
	for _, input := range []string{"", "yesterday", "2024-13-45", "10:00"} {
		t.Run(input, func(t *testing.T) {
			_, err := parseTimeFlag(input, time.Now())
			assert.Error(t, err)
		})
	}
}

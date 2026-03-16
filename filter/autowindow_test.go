package filter

import (
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestDetectAutoWindow(t *testing.T) {
	t.Run("FindsIncidentPeak", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		var entries []types.FilteredEntry

		// 120 minutes of sparse warnings (1 per minute)
		for i := 0; i < 120; i++ {
			entries = append(entries, types.FilteredEntry{
				LogEntry: types.LogEntry{
					Timestamp: base.Add(time.Duration(i) * time.Minute),
					Level:     types.LevelWarn,
					Source:    "app",
					Message:   "warning message",
				},
			})
		}

		// Dense errors in minutes 60-70 (10 per minute)
		for i := 60; i <= 70; i++ {
			for j := 0; j < 10; j++ {
				entries = append(entries, types.FilteredEntry{
					LogEntry: types.LogEntry{
						Timestamp: base.Add(time.Duration(i)*time.Minute + time.Duration(j)*time.Second),
						Level:     types.LevelError,
						Source:    "app",
						Message:   "error message",
					},
				})
			}
		}

		result, dropped := DetectAutoWindow(entries, 15*time.Minute)

		// All errors should survive
		errorCount := 0
		for _, e := range result {
			if e.Level == types.LevelError {
				errorCount++
			}
		}
		assert.Equal(t, 110, errorCount, "all error entries should survive")

		// Some entries should have been dropped (the distant warnings)
		assert.Greater(t, dropped, int64(0), "some entries should be dropped")

		// Result should be smaller than input
		assert.Less(t, len(result), len(entries))
	})

	t.Run("NoErrors", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		var entries []types.FilteredEntry

		// Only warnings, no errors
		for i := 0; i < 60; i++ {
			entries = append(entries, types.FilteredEntry{
				LogEntry: types.LogEntry{
					Timestamp: base.Add(time.Duration(i) * time.Minute),
					Level:     types.LevelWarn,
					Source:    "app",
					Message:   "warning only",
				},
			})
		}

		result, dropped := DetectAutoWindow(entries, 15*time.Minute)

		// No trimming should happen
		assert.Equal(t, len(entries), len(result))
		assert.Equal(t, int64(0), dropped)
	})

	t.Run("AllWithinWindow", func(t *testing.T) {
		base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		var entries []types.FilteredEntry

		// 5 minutes of errors with 15min padding → everything fits
		for i := 0; i < 5; i++ {
			entries = append(entries, types.FilteredEntry{
				LogEntry: types.LogEntry{
					Timestamp: base.Add(time.Duration(i) * time.Minute),
					Level:     types.LevelError,
					Source:    "app",
					Message:   "error",
				},
			})
		}

		result, dropped := DetectAutoWindow(entries, 15*time.Minute)

		assert.Equal(t, len(entries), len(result))
		assert.Equal(t, int64(0), dropped)
	})
}

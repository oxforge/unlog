package filter

import (
	"time"

	"github.com/oxforge/unlog/types"
)

// DetectAutoWindow finds the densest cluster of ERROR/FATAL entries, expands
// outward while the rate stays above 10% of peak, adds padding on both sides,
// and returns only entries within that window. Entries with zero timestamps are
// always kept. If there are no ERROR/FATAL entries, the original slice is
// returned unchanged.
//
// Limitation: expansion stops at the first gap below 10% of peak rate. Incidents
// with disjoint error bursts separated by a quiet period will only capture the
// cluster containing the peak. Multi-phase failures may lose later phases.
func DetectAutoWindow(entries []types.FilteredEntry, padding time.Duration) ([]types.FilteredEntry, int64) {
	histogram := make(map[int64]int64)
	for i := range entries {
		e := &entries[i]
		if e.Timestamp.IsZero() {
			continue
		}
		if e.Level != types.LevelError && e.Level != types.LevelFatal {
			continue
		}
		minute := e.Timestamp.Unix() / 60
		histogram[minute]++
	}

	if len(histogram) == 0 {
		return entries, 0
	}

	var peakMinute int64
	var peakCount int64
	for m, c := range histogram {
		if c > peakCount {
			peakCount = c
			peakMinute = m
		}
	}

	threshold := float64(peakCount) * 0.1
	windowStart := peakMinute
	windowEnd := peakMinute

	for m := peakMinute - 1; ; m-- {
		if float64(histogram[m]) > threshold {
			windowStart = m
		} else {
			break
		}
	}

	for m := peakMinute + 1; ; m++ {
		if float64(histogram[m]) > threshold {
			windowEnd = m
		} else {
			break
		}
	}

	winStart := time.Unix(windowStart*60, 0).Add(-padding)
	winEnd := time.Unix((windowEnd+1)*60, 0).Add(padding)

	var result []types.FilteredEntry
	var dropped int64
	for i := range entries {
		e := &entries[i]
		if e.Timestamp.IsZero() || (!e.Timestamp.Before(winStart) && !e.Timestamp.After(winEnd)) {
			result = append(result, *e)
		} else {
			dropped++
		}
	}

	return result, dropped
}

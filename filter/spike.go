package filter

import (
	"sort"

	"github.com/oxforge/unlog/types"
)

// sourceStats tracks per-second event counts for a single source.
type sourceStats struct {
	buckets map[int64]int64 // unix-second → count
	indices []int           // indices into the entries slice for this source
}

// DetectSpikes scans entries for per-source rate spikes.
// It mutates entries in place, setting IsSpike=true on spike entries.
// Returns the count of entries flagged as spikes.
// A spike is detected when the per-second rate exceeds multiplier * threshold,
// where threshold is the median rate (falling back to mean if median is 0).
// Sources with less than 10 seconds of data are skipped.
func DetectSpikes(entries []types.FilteredEntry, multiplier float64) int64 {
	sources := make(map[string]*sourceStats)
	for i := range entries {
		ts := entries[i].Timestamp
		if ts.IsZero() {
			continue
		}
		src := entries[i].Source
		ss, ok := sources[src]
		if !ok {
			ss = &sourceStats{buckets: make(map[int64]int64)}
			sources[src] = ss
		}
		sec := ts.Unix()
		ss.buckets[sec]++
		ss.indices = append(ss.indices, i)
	}

	var total int64
	for _, ss := range sources {
		if len(ss.buckets) < 10 {
			continue
		}

		threshold := medianRate(ss.buckets)
		if threshold == 0 {
			threshold = meanRate(ss.buckets)
		}
		if threshold == 0 {
			continue
		}

		spikeThreshold := multiplier * threshold

		spikeBuckets := make(map[int64]bool)
		for sec, count := range ss.buckets {
			if float64(count) > spikeThreshold {
				spikeBuckets[sec] = true
			}
		}

		for _, idx := range ss.indices {
			ts := entries[idx].Timestamp
			if spikeBuckets[ts.Unix()] {
				entries[idx].IsSpike = true
				total++
			}
		}
	}

	return total
}

// medianRate returns the median of bucket counts.
func medianRate(buckets map[int64]int64) float64 {
	if len(buckets) == 0 {
		return 0
	}
	vals := make([]int64, 0, len(buckets))
	for _, v := range buckets {
		vals = append(vals, v)
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	n := len(vals)
	if n%2 == 0 {
		return float64(vals[n/2-1]+vals[n/2]) / 2.0
	}
	return float64(vals[n/2])
}

// meanRate returns the mean of bucket counts.
func meanRate(buckets map[int64]int64) float64 {
	if len(buckets) == 0 {
		return 0
	}
	var sum int64
	for _, v := range buckets {
		sum += v
	}
	return float64(sum) / float64(len(buckets))
}

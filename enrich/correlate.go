package enrich

import (
	"log/slog"
	"sort"
	"time"

	"github.com/oxforge/unlog/types"
)

// recentEntry tracks a recent entry's source and timestamp for correlation.
type recentEntry struct {
	Timestamp time.Time
	Source    string
}

// maxRecentEntries caps the correlator buffer to prevent unbounded memory growth.
const maxRecentEntries = 10_000

// Correlator detects temporal correlation between events from different sources.
type Correlator struct {
	window      time.Duration
	recent      []recentEntry
	maxTimeSeen time.Time
	seen        map[string]bool // reused across Correlate calls to reduce allocations
}

// NewCorrelator creates a Correlator with the given correlation window.
func NewCorrelator(window time.Duration) *Correlator {
	return &Correlator{
		window: window,
	}
}

// Correlate sets CorrelatedWith on the entry based on other sources
// that have entries within the correlation window.
func (c *Correlator) Correlate(entry *types.EnrichedEntry) {
	ts := entry.Timestamp
	if ts.After(c.maxTimeSeen) {
		c.maxTimeSeen = ts
	}

	n := 0
	for _, r := range c.recent {
		if c.maxTimeSeen.Sub(r.Timestamp) <= c.window {
			c.recent[n] = r
			n++
		}
	}
	c.recent = c.recent[:n]

	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	for k := range c.seen {
		delete(c.seen, k)
	}
	seen := c.seen
	for _, r := range c.recent {
		if r.Source != entry.Source && !seen[r.Source] {
			diff := ts.Sub(r.Timestamp)
			if diff < 0 {
				diff = -diff
			}
			if diff <= c.window {
				seen[r.Source] = true
			}
		}
	}

	if len(seen) > 0 {
		sources := make([]string, 0, len(seen))
		for s := range seen {
			sources = append(sources, s)
		}
		sort.Strings(sources)
		entry.CorrelatedWith = sources
	}

	if len(c.recent) >= maxRecentEntries {
		slog.Debug("enrich: correlator buffer truncated", "dropped", len(c.recent)/2, "remaining", len(c.recent)-len(c.recent)/2)
		c.recent = c.recent[len(c.recent)/2:]
	}
	c.recent = append(c.recent, recentEntry{
		Timestamp: ts,
		Source:    entry.Source,
	})
}

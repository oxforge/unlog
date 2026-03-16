package filter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterPipeline_Integration_IncidentScenario(t *testing.T) {
	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	input := make(chan types.LogEntry, 200)
	output := make(chan types.FilteredEntry, 200)

	// 100 INFO entries — should be dropped by level filter.
	for i := 0; i < 100; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     types.LevelInfo,
			Source:    "web-api",
			Message:   fmt.Sprintf("request handled in %dms", 50+i),
		}
	}

	// 50 health checks at ERROR level — should be dropped by noise filter.
	for i := 0; i < 50; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(100+i) * time.Second),
			Level:     types.LevelError,
			Source:    "lb",
			Message:   fmt.Sprintf("GET /healthz returned 503 attempt %d", i),
		}
	}

	// 20 similar DB errors with different IPs — fuzzy deduped, maxDups=3.
	for i := 0; i < 20; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(150+i) * time.Second),
			Level:     types.LevelError,
			Source:    "db-proxy",
			Message:   fmt.Sprintf("Connection to 10.0.%d.%d:5432 timed out after 30s", i/256, i%256),
		}
	}

	// 1 FATAL entry — must survive.
	input <- types.LogEntry{
		Timestamp: base.Add(170 * time.Second),
		Level:     types.LevelFatal,
		Source:    "payment-svc",
		Message:   "out of memory: cannot allocate heap",
	}

	close(input)

	opts := FilterOptions{
		MinLevel:            types.LevelWarn,
		Workers:             4,
		MaxDuplicates:       3,
		DedupShards:         4,
		FuzzyDedupCacheSize: 100,
		MaxSurvivors:        10000,
		SpikeMultiplier:     10,
		IsStdin:             true,
		AutoWindow:          false, // disabled since IsStdin=true won't use it, be explicit
	}

	p := NewFilterPipeline(input, output, opts, nil)

	fs, detailed, err := p.Run(context.Background())
	require.NoError(t, err)

	// Collect all output.
	var results []types.FilteredEntry
	for fe := range output {
		results = append(results, fe)
	}

	// Invariant: TotalIngested == TotalDropped + TotalSurvived.
	assert.Equal(t, fs.TotalIngested, fs.TotalDropped+fs.TotalSurvived,
		"invariant: ingested == dropped + survived")
	assert.Equal(t, int64(171), fs.TotalIngested)

	// Verify per-filter drops.
	assert.Greater(t, detailed.DroppedByLevel, int64(0), "should have dropped entries by level")
	assert.Greater(t, detailed.DroppedByNoise, int64(0), "should have dropped entries by noise")
	assert.Greater(t, detailed.DroppedByDedup, int64(0), "should have dropped entries by dedup")

	// FATAL entry must survive.
	foundFatal := false
	for _, r := range results {
		if r.Level == types.LevelFatal && r.Message == "out of memory: cannot allocate heap" {
			foundFatal = true
			break
		}
	}
	assert.True(t, foundFatal, "FATAL entry must survive filtering")

	// At least one dedup summary entry with OccurrenceCount > MaxDuplicates.
	foundSummary := false
	for _, r := range results {
		if r.OccurrenceCount > opts.MaxDuplicates {
			foundSummary = true
			break
		}
	}
	assert.True(t, foundSummary, "should have at least one dedup summary entry")
}

package filter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/types"
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

	// 50 health checks at WARN level — should be dropped by noise filter
	// (at ERROR they would be kept: errors are never noise).
	for i := 0; i < 50; i++ {
		input <- types.LogEntry{
			Timestamp: base.Add(time.Duration(100+i) * time.Second),
			Level:     types.LevelWarn,
			Source:    "lb",
			Message:   fmt.Sprintf("GET /healthz returned 503 attempt %d", i),
		}
	}

	// 20 similar DB errors with different IPs — fuzzy merged into one record.
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
		MinLevel:        types.LevelWarn,
		Workers:         4,
		DedupShards:     4,
		SpikeMultiplier: 10,
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

	// 100 info dropped by level, 50 health checks by noise, 20 DB errors
	// share one signature: 19 merged. Survivors: 1 DB record + 1 FATAL.
	assert.Equal(t, int64(100), detailed.DroppedByLevel)
	assert.Equal(t, int64(50), detailed.DroppedByNoise)
	assert.Equal(t, int64(19), detailed.DroppedByDedup)
	assert.Equal(t, int64(2), fs.TotalSurvived)
	assert.Equal(t, int64(169), fs.TotalDropped)
	require.Len(t, results, 2)

	assert.Equal(t, "db-proxy", results[0].Source)
	assert.Equal(t, 20, results[0].OccurrenceCount)
	assert.True(t, base.Add(150*time.Second).Equal(results[0].FirstSeen))
	assert.True(t, base.Add(169*time.Second).Equal(results[0].LastSeen))

	assert.Equal(t, types.LevelFatal, results[1].Level)
	assert.Equal(t, "out of memory: cannot allocate heap", results[1].Message)
	assert.Equal(t, 1, results[1].OccurrenceCount)
}

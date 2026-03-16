package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oxforge/unlog/compact"
	"github.com/oxforge/unlog/enrich"
	"github.com/oxforge/unlog/filter"
	"github.com/oxforge/unlog/types"
)

// generateEntries creates n synthetic LogEntry values with mixed levels and
// sources, suitable for feeding directly into the filter stage. This benchmarks
// the pipeline processing, not file I/O.
func generateEntries(n int) []types.LogEntry {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	sources := []string{"api", "db", "cache", "queue", "worker"}
	messages := []string{
		"Connection to 10.0.1.42:5432 timed out after 30000ms",
		"Request abc12345-1234-5678-9abc-def012345678 failed with status 503",
		"Memory usage at 92%: heap=1847MB, gc_pause=45ms",
		"Circuit breaker opened for payment-service: 10 consecutive failures",
		"Disk usage warning on /var/data/logs: 89% full",
		"DNS resolution failed for db-primary.internal: NXDOMAIN",
		"Rate limit exceeded: 1500 req/s from 192.168.1.100",
		"Queue backlog growing: 45000 pending messages in order-processing",
		"Certificate expiry warning: api.example.com expires in 72h",
		"Deployment v2.3.1 rolling restart: pod api-7f8b9c started",
	}
	levels := []types.Level{
		types.LevelError, types.LevelError, types.LevelWarn,
		types.LevelError, types.LevelWarn, types.LevelError,
		types.LevelWarn, types.LevelWarn, types.LevelWarn,
		types.LevelInfo,
	}

	entries := make([]types.LogEntry, n)
	for i := range entries {
		entries[i] = types.LogEntry{
			Timestamp:  base.Add(time.Duration(i) * 100 * time.Millisecond),
			Level:      levels[i%len(levels)],
			Source:     sources[i%len(sources)],
			Message:    messages[i%len(messages)],
			LineNumber: int64(i + 1),
		}
	}
	return entries
}

// benchBuffer is a fixed channel buffer size for all pipeline benchmarks.
// Smaller than the 100K entries at the largest scale, so back-pressure
// between stages is exercised realistically.
const benchBuffer = 10_000

// runPipelineWithEntries feeds pre-generated entries into the filter→enrich→compact
// stages, bypassing ingest (no file I/O). Uses a fixed buffer size so that
// larger-than-buffer inputs exercise channel back-pressure.
func runPipelineWithEntries(entries []types.LogEntry) error {
	ctx := context.Background()

	ingestCh := make(chan types.LogEntry, benchBuffer)
	filterCh := make(chan types.FilteredEntry, benchBuffer)
	enrichCh := make(chan types.EnrichedEntry, benchBuffer)

	// Feed entries in a goroutine since the channel may be smaller than the input.
	go func() {
		for _, e := range entries {
			ingestCh <- e
		}
		close(ingestCh)
	}()

	// Filter.
	opts := filter.DefaultFilterOptions()
	opts.MinLevel = types.LevelInfo
	opts.Workers = 1 // deterministic for benchmarks
	fp := filter.NewFilterPipeline(ingestCh, filterCh, opts, nil)

	errCh := make(chan error, 3)

	go func() {
		_, _, err := fp.Run(ctx)
		errCh <- err
	}()

	// Enrich.
	go func() {
		ep := enrich.NewEnricher(filterCh, enrichCh, enrich.DefaultOptions())
		errCh <- ep.Run(ctx)
	}()

	// Compact.
	go func() {
		_, err := compact.Run(ctx, enrichCh, compact.Options{TokenBudget: 4096})
		errCh <- err
	}()

	for i := 0; i < 3; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

func BenchmarkPipeline(b *testing.B) {
	for _, size := range []int{1_000, 10_000, 100_000} {
		name := fmt.Sprintf("%dK_entries", size/1000)
		entries := generateEntries(size)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := runPipelineWithEntries(entries); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

package filter

import (
	"fmt"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
)

var (
	sigShort = "Connection refused"
	sigLong  = `2024-01-15T10:00:10Z ERROR [api-gateway] Request 550e8400-e29b-41d4-a716-446655440000 from 192.168.1.100 to /api/v2/users/profile failed with status 503: upstream timeout after 30000ms, trace=abcdef1234567890, host=web-prod-3.us-east-1.internal`
)

func BenchmarkExtractSignature(b *testing.B) {
	b.Run("short", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ExtractSignature(sigShort)
		}
	})

	b.Run("long", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ExtractSignature(sigLong)
		}
	})
}

func BenchmarkDedupCacheLookup(b *testing.B) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Use distinct words so signatures don't collapse after ExtractSignature
	// normalises numbers. Each entry produces a unique signature like
	// "known error alpha on component bravo".
	words := []string{
		"alpha", "bravo", "charlie", "delta", "echo",
		"foxtrot", "golf", "hotel", "india", "juliet",
	}
	seedEntries := make([]types.LogEntry, 1000)
	for i := range seedEntries {
		seedEntries[i] = types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
			Level:     types.LevelError,
			Source:    "test",
			Message:   fmt.Sprintf("known error %s on component %s", words[i%10], words[(i/10)%10]),
		}
	}

	// Build lookup entries: 100 known (same messages as seed) + 100 unknown.
	lookups := make([]types.LogEntry, 200)
	for i := 0; i < 100; i++ {
		lookups[i] = seedEntries[i]
	}
	for i := 0; i < 100; i++ {
		lookups[100+i] = types.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
			Level:     types.LevelError,
			Source:    "test",
			Message:   fmt.Sprintf("novel error %s from endpoint %s", words[i%10], words[(i/10)%10]),
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Fresh cache each iteration to avoid state leak across b.N runs.
		b.StopTimer()
		dedup := NewDedupFilter(5, 16, 10_000)
		for j := range seedEntries {
			dedup.Apply(seedEntries[j])
		}
		b.StartTimer()

		for j := range lookups {
			dedup.Apply(lookups[j])
		}
	}
}

func BenchmarkNoiseFilter(b *testing.B) {
	nf, err := NewNoiseFilter("")
	if err != nil {
		b.Fatal(err)
	}

	entry := types.LogEntry{
		Level:   types.LevelInfo,
		Message: "Connection pool exhausted: 100 active connections on db-primary:5432",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nf.Filter(entry)
	}
}

func BenchmarkLevelFilter(b *testing.B) {
	lf := NewLevelFilter(types.LevelWarn)
	entries := []types.LogEntry{
		{Level: types.LevelDebug, Message: "debug msg"},
		{Level: types.LevelInfo, Message: "info msg"},
		{Level: types.LevelWarn, Message: "warn msg"},
		{Level: types.LevelError, Message: "error msg"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range entries {
			lf.Filter(entries[j])
		}
	}
}

func BenchmarkSpikeDetection(b *testing.B) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	sources := []string{"api", "db", "cache", "queue", "worker"}

	// Build 10K entries across 5 sources and ~100 seconds with burst patterns.
	// Normal rate: ~20 entries/second across all sources.
	// Burst: seconds 40-42 and 80-82 get 10x normal rate to trigger spike detection.
	entries := make([]types.FilteredEntry, 0, 10_000)
	idx := 0
	for sec := 0; sec < 100; sec++ {
		countPerSec := 20
		if (sec >= 40 && sec <= 42) || (sec >= 80 && sec <= 82) {
			countPerSec = 200 // 10x burst
		}
		for j := 0; j < countPerSec && idx < 10_000; j++ {
			entries = append(entries, types.FilteredEntry{
				LogEntry: types.LogEntry{
					Timestamp: base.Add(time.Duration(sec)*time.Second + time.Duration(j)*time.Millisecond),
					Level:     types.LevelError,
					Source:    sources[idx%len(sources)],
					Message:   "error occurred in request handler",
				},
				Signature: "error occurred in request handler",
			})
			idx++
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset spike flags before each iteration.
		for j := range entries {
			entries[j].IsSpike = false
		}
		DetectSpikes(entries, 10.0)
	}
}

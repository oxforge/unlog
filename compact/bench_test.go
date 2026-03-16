package compact

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
)

func makeEnrichedEntries(n int) []types.EnrichedEntry {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	entries := make([]types.EnrichedEntry, n)
	for i := range entries {
		lvl := types.LevelError
		switch i % 5 {
		case 0:
			lvl = types.LevelFatal
		case 1, 2:
			lvl = types.LevelError
		case 3:
			lvl = types.LevelWarn
		case 4:
			lvl = types.LevelInfo
		}
		entries[i] = types.EnrichedEntry{
			FilteredEntry: types.FilteredEntry{
				LogEntry: types.LogEntry{
					Timestamp: base.Add(time.Duration(i) * 100 * time.Millisecond),
					Level:     lvl,
					Source:    []string{"api", "db", "cache"}[i%3],
					Message:   fmt.Sprintf("Connection failed to host db-%d: timeout after 30s", i%10),
				},
				OccurrenceCount: (i % 5) + 1,
				IsSpike:         i%20 == 0,
				Signature:       "Connection failed to host <PATH>: timeout after <NUM>s",
			},
			ChainID:      map[bool]string{true: "chain-1", false: ""}[i%7 == 0],
			IsDeployment: i%15 == 0,
			HTTPStatus:   500,
			ErrorType:    "ConnectionTimeout",
		}
	}
	return entries
}

func BenchmarkPriorityScoring(b *testing.B) {
	entries := makeEnrichedEntries(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range entries {
			Score(entries[j])
		}
	}
}

func BenchmarkTokenEstimation(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		name := fmt.Sprintf("%dB", size)
		s := strings.Repeat("x", size)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				EstimateTokens(s)
			}
		})
	}
}

func BenchmarkCompactRun(b *testing.B) {
	entries := makeEnrichedEntries(500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := make(chan types.EnrichedEntry, len(entries))
		for _, e := range entries {
			ch <- e
		}
		close(ch)
		_, _ = Run(context.Background(), ch, Options{TokenBudget: 4096})
	}
}

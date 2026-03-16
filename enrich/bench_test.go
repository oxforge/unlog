package enrich

import (
	"context"
	"testing"
	"time"

	"github.com/oxforge/unlog/types"
)

func BenchmarkFieldExtraction(b *testing.B) {
	fe := NewFieldExtractor()
	entry := types.EnrichedEntry{
		FilteredEntry: types.FilteredEntry{
			LogEntry: types.LogEntry{
				Message:  "HTTP/1.1 500 Error: ConnectionTimeout trace_id=abc123def456",
				Metadata: map[string]string{"status": "500"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.HTTPStatus = 0
		entry.ErrorType = ""
		entry.TraceID = ""
		fe.Extract(&entry)
	}
}

func BenchmarkChainMatching(b *testing.B) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	messages := []string{
		"deadlock detected on table orders",
		"connection pool exhausted",
		"query failed: cannot execute",
		"GET /api/users 200 OK",
		"memory warning: heap usage exceeds 90%",
		"timeout connecting to upstream",
		"circuit breaker open for payment-api",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm := NewChainMatcher()
		for j, msg := range messages {
			entry := types.EnrichedEntry{
				FilteredEntry: types.FilteredEntry{
					LogEntry: types.LogEntry{
						Timestamp:  base.Add(time.Duration(j) * time.Second),
						Level:      types.LevelError,
						Source:     "test",
						Message:    msg,
						LineNumber: int64(j),
					},
				},
			}
			cm.Match(&entry)
		}
	}
}

func BenchmarkCorrelation(b *testing.B) {
	sources := []string{"api", "db", "cache", "queue", "worker"}
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := NewCorrelator(5 * time.Second)
		for j := 0; j < 100; j++ {
			entry := types.EnrichedEntry{
				FilteredEntry: types.FilteredEntry{
					LogEntry: types.LogEntry{
						Timestamp: base.Add(time.Duration(j) * 500 * time.Millisecond),
						Source:    sources[j%len(sources)],
						Message:   "error occurred",
					},
				},
			}
			c.Correlate(&entry)
		}
	}
}

func BenchmarkEnrichPipeline(b *testing.B) {
	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	entries := make([]types.FilteredEntry, 1000)
	for i := range entries {
		entries[i] = types.FilteredEntry{
			LogEntry: types.LogEntry{
				Timestamp:  base.Add(time.Duration(i) * 100 * time.Millisecond),
				Level:      types.LevelError,
				Source:     []string{"api", "db", "cache"}[i%3],
				Message:    "HTTP/1.1 500 Error: ConnectionTimeout trace_id=abc123",
				LineNumber: int64(i),
				Metadata:   map[string]string{"status": "500"},
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := make(chan types.FilteredEntry, len(entries))
		output := make(chan types.EnrichedEntry, len(entries))

		for _, e := range entries {
			input <- e
		}
		close(input)

		ep := NewEnricher(input, output, DefaultOptions())
		_ = ep.Run(context.Background())

		// Drain output.
		for range output {
		}
	}
}

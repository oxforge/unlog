package ingest

import (
	"context"
	"os"
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

// TestAllFormats verifies that every testdata file is detected as the correct
// format, produces the expected number of entries, and has accurate level counts.
func TestAllFormats(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		format string
		count  int
		levels map[types.Level]int
	}{
		// --- JSON variants ---
		{
			name:   "json structured",
			file:   "../testdata/formats/json_structured.log",
			format: "json",
			count:  7,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 3,
				types.LevelFatal: 1,
			},
		},
		{
			name:   "json severity field",
			file:   "../testdata/formats/json_severity.json",
			format: "json",
			count:  7,
			levels: map[types.Level]int{
				types.LevelDebug: 1,
				types.LevelInfo:  2,
				types.LevelWarn:  1, // WARNING → Warn
				types.LevelError: 2,
				types.LevelFatal: 1, // CRITICAL → Fatal
			},
		},
		{
			name:   "json epoch timestamps",
			file:   "../testdata/formats/json_epoch.json",
			format: "json",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 2,
				types.LevelFatal: 1,
			},
		},
		{
			name:   "json with stacktrace in field",
			file:   "../testdata/formats/json_multiline_stack.log",
			format: "json",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 2,
				types.LevelFatal: 1,
			},
		},

		// --- CSV variants ---
		{
			name:   "csv with level column (.csv ext)",
			file:   "../testdata/formats/csv_with_levels.csv",
			format: "csv",
			count:  8,
			levels: map[types.Level]int{
				types.LevelDebug: 1,
				types.LevelInfo:  3,
				types.LevelWarn:  1,
				types.LevelError: 2,
				types.LevelFatal: 1,
			},
		},
		{
			name:   "csv structured (.log ext, content detection)",
			file:   "../testdata/formats/csv_structured.log",
			format: "csv",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 2,
				types.LevelFatal: 1,
			},
		},
		{
			name:   "csv inferred levels (.csv ext)",
			file:   "../testdata/formats/csv_inferred_levels.csv",
			format: "csv",
			count:  10,
			levels: map[types.Level]int{
				types.LevelDebug:   1,
				types.LevelInfo:    2,
				types.LevelWarn:    2, // WARN + WARNING
				types.LevelError:   2,
				types.LevelFatal:   1,
				types.LevelUnknown: 2, // "Request processed..." and "Health check..."
			},
		},

		{
			// Kibana CSV export: quoted @timestamp, no level column, levels
			// embedded in Java-style messages like "[Thread-1] INFO com.example...".
			// Extension .csv triggers CSV format. @timestamp parsed via Kibana
			// timestamp format. Levels inferred from message body.
			name:   "csv kibana export (quoted, @timestamp, no level column)",
			file:   "../testdata/formats/csv_kibana.csv",
			format: "csv",
			count:  20,
			levels: map[types.Level]int{
				types.LevelFatal:   1, // FATAL in message
				types.LevelError:   4, // ERROR ×3 + [error] ×1
				types.LevelWarn:    2, // WARN ×2
				types.LevelInfo:    3, // INFO ×3
				types.LevelDebug:   1, // DEBUG ×1
				types.LevelUnknown: 9, // stacktrace lines, exception lines, Caused by
			},
		},

		// --- Docker JSON ---
		{
			// Docker parser: stdout → Info, stderr → Error, then first-word scan.
			// WARN:/ERROR:/FATAL: in first word override the stream default.
			name:   "docker json",
			file:   "../testdata/formats/docker_json.log",
			format: "docker-json",
			count:  7,
			levels: map[types.Level]int{
				types.LevelInfo:  3, // Starting, Listening, Connected (stdout)
				types.LevelWarn:  1, // "WARN:" overrides stderr→Error
				types.LevelError: 2, // "ERROR:", "Attempting..." (stderr default)
				types.LevelFatal: 1, // "FATAL:" overrides stderr→Error
			},
		},
		{
			// Docker parser: messages start with "[2024-..." so first-word scan
			// finds "[2024-01-15" not a level keyword. Stream defaults apply.
			name:   "docker mixed streams with timestamps in message",
			file:   "../testdata/formats/docker_mixed_streams.log",
			format: "docker-json",
			count:  9,
			levels: map[types.Level]int{
				types.LevelInfo:  3, // 3 stdout lines
				types.LevelError: 6, // 6 stderr lines (first word is timestamp or traceback, not level keyword)
			},
		},

		// --- Kubernetes ---
		{
			// Kube parser: stdout → Info, stderr → Error, then first-word scan.
			name:   "kubernetes",
			file:   "../testdata/formats/kubernetes.log",
			format: "kubernetes",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  3, // stdout: Starting, Listening, Health
				types.LevelWarn:  1, // "WARN:" overrides stderr
				types.LevelError: 1, // "ERROR:" (already Error from stderr)
				types.LevelFatal: 1, // "FATAL:" overrides stderr
			},
		},
		{
			// All lines match kube regex (including P=partial lines), each is an entry.
			name:   "kubernetes multiline (partial lines are entries)",
			file:   "../testdata/formats/kubernetes_multiline.log",
			format: "kubernetes",
			count:  10,
			levels: map[types.Level]int{
				types.LevelInfo:  2, // stdout: Starting, Health
				types.LevelWarn:  1, // "WARN:" on stderr
				types.LevelError: 6, // stderr default: ERROR:, panic:, [signal, goroutine, main.(*), \t/app/
				types.LevelFatal: 1, // "FATAL:" on stderr
			},
		},

		// --- Logfmt ---
		{
			name:   "logfmt",
			file:   "../testdata/formats/logfmt.log",
			format: "logfmt",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 3,
			},
		},
		{
			name:   "logfmt varied keys (time/lvl instead of ts/level)",
			file:   "../testdata/formats/logfmt_varied.log",
			format: "logfmt",
			count:  7,
			levels: map[types.Level]int{
				types.LevelDebug: 1,
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 2,
				types.LevelFatal: 1,
			},
		},

		// --- Syslog ---
		{
			name:   "syslog rfc3164",
			file:   "../testdata/formats/syslog.log",
			format: "syslog-rfc3164",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2, // priority 134 → severity 6 → Info
				types.LevelWarn:  1, // priority 132 → severity 4 → Warn
				types.LevelError: 2, // priority 131 → severity 3 → Error (Warning + ERROR lines)
				types.LevelFatal: 1, // priority 130 → severity 2 → Fatal
			},
		},
		{
			// Syslog 5424 severity = priority % 8
			name:   "syslog rfc5424",
			file:   "../testdata/formats/syslog_rfc5424.log",
			format: "syslog-rfc5424",
			count:  6,
			levels: map[types.Level]int{
				types.LevelWarn:  3, // 165%8=5, 165%8=5, 164%8=4 → Warn
				types.LevelError: 1, // 163%8=3 → Error
				types.LevelFatal: 2, // 162%8=2, 161%8=1 → Fatal
			},
		},

		// --- CLF ---
		{
			// CLF maps HTTP status: 2xx → Info, 4xx → Warn, 5xx → Error
			name:   "apache clf",
			file:   "../testdata/formats/apache_clf.log",
			format: "clf",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2, // 200, 200
				types.LevelWarn:  1, // 404
				types.LevelError: 3, // 500, 500, 503
			},
		},

		// --- CloudWatch ---
		{
			// CloudWatch parser does first-word scan on @message.
			// "WARN:", "ERROR:" match. Others stay Unknown → InferLevel runs
			// but finds no level keywords.
			name:   "cloudwatch json",
			file:   "../testdata/formats/cloudwatch.log",
			format: "cloudwatch",
			count:  6,
			levels: map[types.Level]int{
				types.LevelUnknown: 3, // Starting, Processing, Retrying
				types.LevelWarn:    1, // "WARN:"
				types.LevelError:   2, // "ERROR:" ×2
			},
		},

		// --- Generic timestamps ---
		{
			name:   "generic timestamps",
			file:   "../testdata/formats/generic_timestamp.log",
			format: "generic",
			count:  6,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelWarn:  1,
				types.LevelError: 2,
				types.LevelFatal: 1,
			},
		},
		{
			// Timestamps with bracketed levels like "[INFO]". The generic parser's
			// first-word check doesn't strip brackets, but InferLevel does.
			name:   "generic with brackets",
			file:   "../testdata/formats/generic_with_brackets.log",
			format: "generic",
			count:  10,
			levels: map[types.Level]int{
				types.LevelInfo:  2,
				types.LevelDebug: 1,
				types.LevelWarn:  2,
				types.LevelError: 4,
				types.LevelFatal: 1,
			},
		},

		// --- Raw / unstructured ---
		{
			// Raw parser sets LevelUnknown → InferLevel scans messages.
			// "Warning:" → Warn, "Error:" → Error ×2, "Fatal:" → Fatal.
			// Numeric tokens like "5" in "in 5 seconds" are skipped.
			name:   "unstructured raw",
			file:   "../testdata/formats/unstructured.log",
			format: "raw",
			count:  8,
			levels: map[types.Level]int{
				types.LevelUnknown: 4, // no keywords found
				types.LevelWarn:    1, // "Warning:"
				types.LevelError:   2, // "Error:" ×2
				types.LevelFatal:   1, // "Fatal:"
			},
		},
		{
			name:   "raw with level keywords",
			file:   "../testdata/formats/raw_with_levels.log",
			format: "raw",
			count:  9,
			levels: map[types.Level]int{
				types.LevelInfo:    1,
				types.LevelWarn:    1,
				types.LevelError:   2,
				types.LevelFatal:   1,
				types.LevelUnknown: 4,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := make(chan types.LogEntry, 1000)
			ingester := NewIngester(output, IngestOptions{SampleLines: 100})

			f, err := os.Open(tt.file)
			if err != nil {
				t.Fatalf("open %s: %v", tt.file, err)
			}
			defer func() { _ = f.Close() }()

			err = ingester.processSource(context.Background(), tt.file, f)
			close(output)
			assert.NoError(t, err)

			var entries []types.LogEntry
			for e := range output {
				entries = append(entries, e)
			}

			stats := ingester.SourceStats()
			ss, ok := stats[tt.file]
			assert.True(t, ok, "source stats should contain %s", tt.file)

			// Verify format detection.
			assert.Equal(t, tt.format, ss.Format, "format for %s", tt.file)

			// Verify entry count.
			assert.Equal(t, tt.count, len(entries), "entry count for %s", tt.file)

			// Verify level distribution.
			gotLevels := make(map[types.Level]int)
			for _, e := range entries {
				gotLevels[e.Level]++
			}
			assert.Equal(t, tt.levels, gotLevels, "level counts for %s", tt.file)
		})
	}
}

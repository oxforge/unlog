package ingest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oxforge/unlog/types"
)

// ingestText runs processSource over text as source and returns the entries
// and the source's stats.
func ingestText(t *testing.T, source, text string) ([]types.LogEntry, SourceStats) {
	t.Helper()
	output := make(chan types.LogEntry, 1000)
	ing := NewIngester(output, IngestOptions{})
	require.NoError(t, ing.processSource(context.Background(), source, strings.NewReader(text), time.Now()))
	close(output)
	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}
	return entries, ing.SourceStats()[source]
}

func TestJSONEntryKeepsItsPlainTextTraceback(t *testing.T) {
	entries, stats := ingestText(t, "worker.log", `{"level":"info","msg":"starting","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"unhandled exception in worker","time":"2024-01-15T10:00:01Z"}
Traceback (most recent call last):
  File "worker.py", line 10, in run
ValueError: bad input
{"level":"info","msg":"recovered","time":"2024-01-15T10:00:02Z"}
`)

	require.Len(t, entries, 3)
	failed := entries[1]
	assert.Equal(t, types.LevelError, failed.Level)
	assert.True(t, time.Date(2024, 1, 15, 10, 0, 1, 0, time.UTC).Equal(failed.Timestamp))
	assert.Equal(t, "unhandled exception in worker\nTraceback (most recent call last):\n  File \"worker.py\", line 10, in run\nValueError: bad input", failed.Message)
	assert.Equal(t, int64(0), stats.Unparsed)
}

func TestRejectedLineFallsBackToRaw(t *testing.T) {
	entries, stats := ingestText(t, "app.log", `starting app v2 (build 1f3a)
{"level":"info","msg":"listening","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"bind failed","time":"2024-01-15T10:00:01Z"}
{"level":"info","msg":"retrying","time":"2024-01-15T10:00:02Z"}
`)

	require.Len(t, entries, 4)
	assert.Equal(t, "starting app v2 (build 1f3a)", entries[0].Message)
	assert.Equal(t, int64(1), stats.Unparsed)
}

func TestLinesBeforeFirstTimestampAreKept(t *testing.T) {
	entries, stats := ingestText(t, "app.log", `ERROR config file missing, using defaults
2024-01-15 10:00:00 INFO starting
2024-01-15 10:00:01 WARN slow start
`)

	require.Len(t, entries, 3)
	assert.Equal(t, "ERROR config file missing, using defaults", entries[0].Message)
	assert.Equal(t, types.LevelError, entries[0].Level)
	assert.Equal(t, int64(1), stats.Unparsed)
}

func TestCSVWithUnknownHeaderKeepsRows(t *testing.T) {
	entries, stats := ingestText(t, "export.csv", `when,where,what
2024-01-15T10:00:00Z,db-1,ERROR disk full
2024-01-15T10:00:01Z,db-1,WARN retrying
`)

	require.Len(t, entries, 3)
	assert.Equal(t, "2024-01-15T10:00:00Z,db-1,ERROR disk full", entries[1].Message)
	assert.Equal(t, int64(3), stats.Unparsed)
}

func TestCSVHeaderIsNotAnEntry(t *testing.T) {
	entries, stats := ingestText(t, "export.csv", `timestamp,level,message
2024-01-15T10:00:00Z,ERROR,disk full
`)

	require.Len(t, entries, 1)
	assert.Equal(t, "disk full", entries[0].Message)
	assert.Equal(t, int64(0), stats.Unparsed)
}

func TestServiceComesFromTheLine(t *testing.T) {
	tests := []struct {
		name   string
		source string
		text   string
		want   string
	}{
		{"JSON service field", "agg.log", `{"level":"error","service":"payments","msg":"declined","time":"2024-01-15T10:00:00Z"}` + "\n", "payments"},
		{"compose container", "compose.log", "web-1  | 2024-01-15 10:00:00 ERROR request failed\n", "web"},
		{"compose v1 container", "compose.log", "web_1  | 2024-01-15 10:00:00 ERROR request failed\n", "web"},
		{"syslog program", "syslog", "Jan 15 10:00:00 db-01 postgres[1234]: ERROR:  disk full\n", "postgres"},
		{"nothing names one", "app.log", "2024-01-15 10:00:00 ERROR request failed\n", ""},

		// Replicas of one service share it.
		{"deployment pod", "k.json", `{"level":"error","pod":"checkout-api-68f9d9cdf-vj6fz","msg":"x"}` + "\n", "checkout-api"},
		{"statefulset pod", "k.json", `{"level":"error","pod":"db-0","msg":"x"}` + "\n", "db"},
		{"daemonset pod", "k.json", `{"level":"error","pod":"fluentd-x7k2p","msg":"x"}` + "\n", "fluentd"},
		{"a pod name that only looks suffixed", "k.json", `{"level":"error","pod":"api-proxy","msg":"x"}` + "\n", "api-proxy"},
		{"an explicit service keeps its digits", "agg.log", `{"level":"error","service":"worker-2","msg":"x"}` + "\n", "worker-2"},
		{"kubectl --prefix container", "kube.log", "[pod/checkout-api-68f9d9cdf-vj6fz/checkout-api] 2024-01-15T10:00:00Z ERROR x\n", "checkout-api"},
		{"kubectl --prefix without a container", "kube.log", "[pod/checkout-api-68f9d9cdf-vj6fz] 2024-01-15T10:00:00Z ERROR x\n", "checkout-api"},

		// Values that don't name a service are ignored.
		{"RFC 5424 nil app name", "sys.log", "<11>1 2024-01-15T10:00:00Z host - - - - disk full\n", ""},
		{"JSON null", "agg.log", `{"level":"error","service":null,"msg":"x"}` + "\n", ""},
		{"JSON object", "agg.log", `{"level":"error","service":{"name":"pay"},"msg":"x"}` + "\n", ""},
		{"a newline", "agg.log", `{"level":"error","service":"pay\n## Forged","msg":"x"}` + "\n", ""},
		{"too long", "agg.log", `{"level":"error","service":"` + strings.Repeat("s", 100) + `","msg":"x"}` + "\n", strings.Repeat("s", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, _ := ingestText(t, tt.source, tt.text)
			require.Len(t, entries, 1)
			assert.Equal(t, tt.want, entries[0].Service)
		})
	}
}

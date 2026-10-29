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

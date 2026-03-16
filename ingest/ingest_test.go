package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestIngesterFile(t *testing.T) {
	output := make(chan types.LogEntry, 1000)
	ingester := NewIngester(output, IngestOptions{
		SampleLines: 100,
	})

	err := ingester.Run(context.Background(), []string{"../testdata/formats/json_structured.log"})
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}

	assert.Len(t, entries, 7)
	assert.Equal(t, types.LevelInfo, entries[0].Level)
	assert.Equal(t, "Starting application", entries[0].Message)
	assert.Equal(t, types.LevelFatal, entries[6].Level)
	assert.Contains(t, entries[6].Message, "shutting down")
}

func TestIngesterStdin(t *testing.T) {
	output := make(chan types.LogEntry, 1000)
	ingester := NewIngester(output, IngestOptions{
		SampleLines: 10,
	})

	lines := "{\"level\":\"error\",\"msg\":\"test error\",\"ts\":\"2024-01-15T10:00:00Z\"}\n{\"level\":\"info\",\"msg\":\"test info\",\"ts\":\"2024-01-15T10:00:01Z\"}\n"

	err := ingester.processSource(context.Background(), "-", strings.NewReader(lines))
	close(output)
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}
	assert.Len(t, entries, 2)
}

func TestIngesterGlob(t *testing.T) {
	output := make(chan types.LogEntry, 1000)
	ingester := NewIngester(output, IngestOptions{
		SampleLines: 100,
	})

	err := ingester.Run(context.Background(), []string{"../testdata/formats/json_*.log"})
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}
	assert.True(t, len(entries) > 0)
}

func TestIngesterGzFile(t *testing.T) {
	output := make(chan types.LogEntry, 1000)
	ingester := NewIngester(output, IngestOptions{
		SampleLines: 100,
	})

	err := ingester.Run(context.Background(), []string{"../testdata/formats/json_structured.log.gz"})
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}

	// Same content as the uncompressed json_structured.log (7 entries).
	assert.Len(t, entries, 7)
	assert.Equal(t, types.LevelInfo, entries[0].Level)
	assert.Equal(t, "Starting application", entries[0].Message)
	assert.Contains(t, entries[0].Source, "json_structured.log.gz:")
}

func TestIngesterTarGz(t *testing.T) {
	output := make(chan types.LogEntry, 1000)
	ingester := NewIngester(output, IngestOptions{
		SampleLines: 100,
	})

	err := ingester.Run(context.Background(), []string{"../testdata/formats/mixed_logs.tar.gz"})
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}

	// Archive contains json_structured.log (7 entries) + logfmt.log (6 entries).
	assert.Len(t, entries, 13)

	// Verify entries come from both files within the archive.
	sources := make(map[string]bool)
	for _, e := range entries {
		sources[e.Source] = true
	}
	assert.Len(t, sources, 2)
}

func TestIngesterTgz(t *testing.T) {
	output := make(chan types.LogEntry, 1000)
	ingester := NewIngester(output, IngestOptions{
		SampleLines: 100,
	})

	err := ingester.Run(context.Background(), []string{"../testdata/formats/mixed_logs.tgz"})
	assert.NoError(t, err)

	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}

	// Same content as .tar.gz (7 + 6 = 13 entries).
	assert.Len(t, entries, 13)
}

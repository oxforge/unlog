package ingest

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oxforge/unlog/types"
)

func touch(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestExpandSourcesFindsRotatedLogsAndSkipsHiddenAndNonLogs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"app.log", "app.log.1", "app.log.2024-01-15", "app.log-20240115", "app.log.3.gz",
		"events.jsonl", "events.ndjson", "syslog.1", "messages",
		".hidden.log", ".git/HEAD", ".git/logs/HEAD", "image.png", "notes.md",
	} {
		touch(t, dir, name, "x\n")
	}

	files, err := ExpandSources([]string{dir})
	require.NoError(t, err)

	var got []string
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		require.NoError(t, err)
		got = append(got, rel)
	}
	sort.Strings(got)
	assert.Equal(t, []string{
		"app.log", "app.log-20240115", "app.log.1", "app.log.2024-01-15", "app.log.3.gz",
		"events.jsonl", "events.ndjson", "messages", "syslog.1",
	}, got)
}

func TestExpandSourcesFailsOnAMissingPath(t *testing.T) {
	existing := touch(t, t.TempDir(), "app.log", "x\n")

	_, err := ExpandSources([]string{existing, filepath.Join(t.TempDir(), "nope.log")})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope.log")
}

func TestIngesterFailsWhenNoLogFilesAreFound(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "notes.md", "x\n")
	output := make(chan types.LogEntry, 10)

	err := NewIngester(output, IngestOptions{}).Run(context.Background(), []string{dir})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no log files found")
}

func TestIngesterSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "app.log", "2024-01-15 10:00:00 ERROR disk full\n")
	touch(t, dir, "wtmp", "\x00\x00\x07\x00ts/0\x00\x00root\x00\x00")
	output := make(chan types.LogEntry, 10)
	ing := NewIngester(output, IngestOptions{})

	require.NoError(t, ing.Run(context.Background(), []string{dir}))

	var sources []string
	for e := range output {
		sources = append(sources, filepath.Base(e.Source))
	}
	assert.Equal(t, []string{"app.log"}, sources)
}

func TestIsLogNameAcceptsRotatedArchiveEntries(t *testing.T) {
	for name, want := range map[string]bool{
		"var/log/app.log.1": true, "app.log-20240115": true, "events.jsonl": true,
		"syslog.2": true, "image.png": false, "report.pdf.1": false,
	} {
		assert.Equal(t, want, isLogName(name), name)
	}
}

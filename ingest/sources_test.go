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

func TestExpandSourcesSkipsCompressedNonLogs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app.log.3.gz", "syslog.7.gz", "backup.sql.gz", "bundle.tgz"} {
		touch(t, dir, name, "x")
	}

	files, err := ExpandSources([]string{dir})

	require.NoError(t, err)
	var got []string
	for _, f := range files {
		got = append(got, filepath.Base(f))
	}
	sort.Strings(got)
	assert.Equal(t, []string{"app.log.3.gz", "bundle.tgz", "syslog.7.gz"}, got)
}

func TestExpandSourcesReadsAFileOnceWhateverItsSpelling(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "logs/app.log", "x\n")
	t.Chdir(dir)

	files, err := ExpandSources([]string{"logs", "./logs/app.log", "logs//app.log"})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join("logs", "app.log")}, files)
}

// A symlinked directory (Capistrano's "current") is walked under the name
// it was given; symlinked directories inside a walk are not followed, so a
// link back up can't loop.
func TestExpandSourcesWalksASymlinkedDirectory(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "releases/1/log/app.log", "x\n")
	require.NoError(t, os.Symlink(filepath.Join(dir, "releases", "1"), filepath.Join(dir, "current")))
	require.NoError(t, os.Symlink(dir, filepath.Join(dir, "releases", "1", "log", "up")))

	files, err := ExpandSources([]string{filepath.Join(dir, "current")})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "current", "log", "app.log")}, files)
}

// A file named on the command line that can't be read is an error, like a
// missing one; the same problem in a file found by walking a directory is
// only a warning.
func TestIngesterFailsOnANamedFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	good := touch(t, dir, "app.log", "2024-01-15 10:00:00 ERROR disk full\n")
	binary := touch(t, dir, "wtmp", wtmpBytes)
	corrupt := touch(t, dir, "old.log.gz", "not gzip")
	utf16 := touch(t, dir, "ps.log", "\xff\xfe2\x000\x00")
	unreadable := touch(t, dir, "secure.log", "2024-01-15 10:00:00 ERROR x\n")
	require.NoError(t, os.Chmod(unreadable, 0))
	if f, err := os.Open(unreadable); err == nil {
		_ = f.Close()
		unreadable = "" // running as root: every file is readable
	}

	for name, path := range map[string]string{"binary": binary, "corrupt gzip": corrupt, "UTF-16": utf16, "unreadable": unreadable} {
		if path == "" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			_, err := runIngest(t, good, path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), filepath.Base(path))
		})
	}

	t.Run("found by walking", func(t *testing.T) {
		entries, err := runIngest(t, dir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, good, entries[0].Source)
	})
}

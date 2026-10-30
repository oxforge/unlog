package ingest

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oxforge/unlog/internal/types"
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

// logrotate's copytruncate leaves a writer that kept its offset writing
// after a run of NUL bytes; the text after them is the log.
func TestIngesterReadsALogTruncatedInPlace(t *testing.T) {
	dir := t.TempDir()
	path := touch(t, dir, "app.log", strings.Repeat("\x00", 20*1024)+"2024-01-15 10:00:00 ERROR disk full\n")

	for name, arg := range map[string]string{"walked": dir, "named": path} {
		t.Run(name, func(t *testing.T) {
			entries, err := runIngest(t, arg)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Contains(t, entries[0].Message, "disk full")
		})
	}
}

// A named directory or archive from which nothing could be read fails like
// a named file would: the run must not pass with "0 errors".
func TestIngesterFailsWhenANamedDirectoryHasNothingReadable(t *testing.T) {
	binaryDir := t.TempDir()
	touch(t, binaryDir, "app.log", wtmpBytes)
	archive := touch(t, t.TempDir(), "bundle.tgz", tarGzipped(t, map[string]string{"var/log/wtmp": wtmpBytes}))

	for name, arg := range map[string]string{"directory": binaryDir, "archive": archive} {
		t.Run(name, func(t *testing.T) {
			_, err := runIngest(t, arg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), arg)
		})
	}

	t.Run("one readable file is enough", func(t *testing.T) {
		touch(t, binaryDir, "ok.log", "2024-01-15 10:00:00 ERROR disk full\n")
		_, err := runIngest(t, binaryDir)
		assert.NoError(t, err)
	})
}

func TestExpandSourcesReadsTheSameFileOnce(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "releases/1/log/app.log", "x\n")
	require.NoError(t, os.Symlink(filepath.Join(dir, "releases", "1"), filepath.Join(dir, "current")))
	t.Chdir(dir)

	files, err := ExpandSources([]string{"current/log", "releases/1/log", filepath.Join(dir, "releases/1/log/app.log")})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join("current", "log", "app.log")}, files)
}

// A symlink to a directory outside the tree is followed once; one back into
// the tree, or to an ancestor, is not, so nothing is read twice or loops.
func TestExpandSourcesFollowsALinkOutOfTheTree(t *testing.T) {
	data, logs := t.TempDir(), t.TempDir()
	touch(t, data, "app/current.log", "x\n")
	touch(t, logs, "sys.log", "x\n")
	require.NoError(t, os.Symlink(filepath.Join(data, "app"), filepath.Join(logs, "app")))
	require.NoError(t, os.Symlink(logs, filepath.Join(data, "app", "back")))
	require.NoError(t, os.Symlink(logs, filepath.Join(logs, "self")))

	files, err := ExpandSources([]string{logs})

	require.NoError(t, err)
	sort.Strings(files)
	assert.Equal(t, []string{filepath.Join(logs, "app", "current.log"), filepath.Join(logs, "sys.log")}, files)
}

// A glob unlog expands itself is a search, like a directory: what it finds
// that can't be read is skipped, not fatal.
func TestIngesterSkipsUnreadableGlobMatches(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "app.log", "2024-01-15 10:00:00 ERROR disk full\n")
	touch(t, dir, "wtmp.log", wtmpBytes)

	entries, err := runIngest(t, filepath.Join(dir, "*.log"))

	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestIngesterExcludesPaths(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "app.log", "2024-01-15 10:00:00 ERROR disk full\n")
	report := touch(t, dir, "report.txt", "2024-01-15 10:00:00 FATAL from an earlier report\n")
	output := make(chan types.LogEntry, 10)

	require.NoError(t, NewIngester(output, IngestOptions{Exclude: []string{report}}).Run(context.Background(), []string{dir}))

	var sources []string
	for e := range output {
		sources = append(sources, filepath.Base(e.Source))
	}
	assert.Equal(t, []string{"app.log"}, sources)
}

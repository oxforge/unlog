package ingest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oxforge/unlog/types"
)

const wtmpBytes = "\x00\x00\x07\x00ts/0\x00\x00root\x00\x00"

func gzipped(t *testing.T, content string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return buf.String()
}

func tarGzipped(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, name := range []string{"var/log/app.log", "var/log/wtmp"} {
		content, ok := files[name]
		if !ok {
			continue
		}
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg, ModTime: time.Now(),
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.String()
}

func runIngest(t *testing.T, sources ...string) ([]types.LogEntry, error) {
	t.Helper()
	output := make(chan types.LogEntry, 1000)
	err := NewIngester(output, IngestOptions{}).Run(context.Background(), sources)
	var entries []types.LogEntry
	for e := range output {
		entries = append(entries, e)
	}
	return entries, err
}

func TestProcessSourceRejectsBinaryAndUTF16(t *testing.T) {
	for name, content := range map[string]string{
		"binary": wtmpBytes,
		"UTF-16": "\xff\xfe2\x000\x002\x004\x00",
	} {
		t.Run(name, func(t *testing.T) {
			output := make(chan types.LogEntry, 10)
			err := NewIngester(output, IngestOptions{}).processSource(context.Background(), "x", strings.NewReader(content), time.Now())
			require.Error(t, err)
			assert.True(t, isNotText(err), "error: %v", err)
			if name == "UTF-16" {
				assert.Contains(t, err.Error(), "UTF-16")
			}
		})
	}
}

func TestIngesterSkipsBinaryInsideArchives(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "bundle.tgz", tarGzipped(t, map[string]string{
		"var/log/app.log": "2024-01-15 10:00:00 ERROR disk full\n",
		"var/log/wtmp":    wtmpBytes,
	}))
	touch(t, dir, "wtmp.1.gz", gzipped(t, wtmpBytes))

	entries, err := runIngest(t, dir)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, filepath.Join(dir, "bundle.tgz")+":var/log/app.log", entries[0].Source)
}

//go:build unix

package ingest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pipe can't seek back, so nothing may be read from it before parsing.
func TestIngesterReadsAPipeFromItsFirstLine(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("2024-01-15T10:00:00Z ERROR the first error\n")
	for i := 0; sb.Len() < 20*1024; i++ {
		fmt.Fprintf(&sb, "2024-01-15T10:00:01Z INFO filler line %d with some padding text\n", i)
	}
	fifo := filepath.Join(t.TempDir(), "pipe")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	go func() {
		f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		_, _ = f.WriteString(sb.String())
		_ = f.Close()
	}()

	entries, err := runIngest(t, fifo)

	require.NoError(t, err)
	require.Equal(t, strings.Count(sb.String(), "\n"), len(entries))
	assert.Contains(t, entries[0].Message, "the first error")
}

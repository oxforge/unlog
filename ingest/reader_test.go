package ingest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReaderSingleLines(t *testing.T) {
	input := "2024-01-15T10:00:00Z first line\n2024-01-15T10:00:01Z second line\n"
	r := newReader(strings.NewReader(input), "test.log", isTimestampLine)
	lines, err := r.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, lines, 2)
	assert.Equal(t, "2024-01-15T10:00:00Z first line", lines[0].Text)
	assert.Equal(t, int64(1), lines[0].LineNumber)
	assert.Equal(t, "2024-01-15T10:00:01Z second line", lines[1].Text)
	assert.Equal(t, int64(2), lines[1].LineNumber)
}

func TestReaderMultiLine(t *testing.T) {
	input := "2024-01-15T10:00:00Z error occurred\n  at com.example.Foo.bar(Foo.java:42)\n  at com.example.Main.main(Main.java:10)\n2024-01-15T10:00:01Z next entry\n"
	r := newReader(strings.NewReader(input), "test.log", isTimestampLine)
	lines, err := r.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[0].Text, "Foo.java:42")
	assert.Contains(t, lines[0].Text, "Main.java:10")
	assert.Equal(t, int64(1), lines[0].LineNumber)
	assert.Equal(t, int64(4), lines[1].LineNumber)
}

func TestReaderEmptyLineBreaksMultiLine(t *testing.T) {
	input := "2024-01-15T10:00:00Z first entry\n  continuation\n\n2024-01-15T10:00:01Z second entry\n"
	r := newReader(strings.NewReader(input), "test.log", isTimestampLine)
	lines, err := r.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[0].Text, "continuation")
}

func TestReaderEOFFlush(t *testing.T) {
	input := "2024-01-15T10:00:00Z only entry"
	r := newReader(strings.NewReader(input), "test.log", isTimestampLine)
	lines, err := r.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, lines, 1)
	assert.Equal(t, "2024-01-15T10:00:00Z only entry", lines[0].Text)
}

func TestReaderEmptyInput(t *testing.T) {
	r := newReader(strings.NewReader(""), "test.log", isTimestampLine)
	lines, err := r.ReadAll()
	assert.NoError(t, err)
	assert.Len(t, lines, 0)
}

func isTimestampLine(line string) bool {
	if len(line) == 0 {
		return false
	}
	return line[0] >= '0' && line[0] <= '9'
}

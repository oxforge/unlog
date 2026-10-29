package ingest

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// maxLineSize caps a physical line and a reassembled multi-line entry, in
// bytes. A longer line keeps its first maxLineSize bytes; a continuation that
// would grow an entry past the cap starts a new entry instead.
const maxLineSize = 1024 * 1024 // 1MB

// RawLine is a log line (or multi-line group) read from a source file.
type RawLine struct {
	Text       string
	LineNumber int64
	Source     string
}

// LineChecker reports whether a line starts a new log entry (as opposed to
// being a continuation of the previous entry, e.g. a stack trace line).
type LineChecker func(line string) bool

// reader wraps an io.Reader and reassembles multi-line log entries before
// handing them to the rest of the pipeline. Continuation lines (those that do
// not pass the isNewEntry check) are appended to the current entry.
type reader struct {
	br         *bufio.Reader
	source     string
	isNewEntry LineChecker
}

// newReader creates a reader that reads from r, tags lines with source, and
// uses isNewEntry to distinguish entry boundaries from continuation lines.
func newReader(r io.Reader, source string, isNewEntry LineChecker) *reader {
	return &reader{
		br:         bufio.NewReaderSize(r, 64*1024),
		source:     source,
		isNewEntry: isNewEntry,
	}
}

// readLine returns the next physical line without its line ending (\n or
// \r\n), cut to maxLineSize bytes. It returns io.EOF when no line is left.
func (r *reader) readLine() (string, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.br.ReadLine()
		if err != nil {
			return "", err
		}
		if room := maxLineSize - len(buf); room > 0 {
			buf = append(buf, chunk[:min(len(chunk), room)]...)
		}
		if !isPrefix {
			return string(buf), nil
		}
	}
}

// ReadAll reads all reassembled lines into a slice.
func (r *reader) ReadAll() ([]RawLine, error) {
	var lines []RawLine
	err := r.ReadFunc(func(line RawLine) {
		lines = append(lines, line)
	})
	return lines, err
}

// ReadFunc calls fn for each reassembled log entry in order. An empty line
// flushes the current entry, preventing unbounded accumulation of continuations.
func (r *reader) ReadFunc(fn func(RawLine)) error {
	return r.ReadFuncContext(context.Background(), func(line RawLine) bool {
		fn(line)
		return true
	})
}

// ReadFuncContext is like ReadFunc but stops early when ctx is cancelled.
// The callback fn returns false to signal that iteration should stop (e.g.
// because the output channel send was pre-empted by context cancellation).
func (r *reader) ReadFuncContext(ctx context.Context, fn func(RawLine) bool) error {
	var current *strings.Builder
	var currentLineNum int64
	var lineNum int64
	stopped := false

	flush := func() {
		if current != nil && !stopped {
			if !fn(RawLine{
				Text:       current.String(),
				LineNumber: currentLineNum,
				Source:     r.source,
			}) {
				stopped = true
			}
			current = nil
		}
	}

	for {
		line, err := r.readLine()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if stopped || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		lineNum++

		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}

		switch {
		case r.isNewEntry(line):
			flush()
			current = &strings.Builder{}
			current.WriteString(line)
			currentLineNum = lineNum
		case current != nil && current.Len()+1+len(line) <= maxLineSize:
			current.WriteByte('\n')
			current.WriteString(line)
		default:
			// A continuation with nothing to attach to, or one that would
			// push the entry past maxLineSize, becomes an entry of its own.
			flush()
			current = &strings.Builder{}
			current.WriteString(line)
			currentLineNum = lineNum
		}
	}

	flush()
	return nil
}

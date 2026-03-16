package ingest

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// maxLineSize is the maximum size of a single log line (including multi-line continuations).
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
	scanner    *bufio.Scanner
	source     string
	isNewEntry LineChecker
}

// newReader creates a reader that reads from r, tags lines with source, and
// uses isNewEntry to distinguish entry boundaries from continuation lines.
func newReader(r io.Reader, source string, isNewEntry LineChecker) *reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	return &reader{
		scanner:    scanner,
		source:     source,
		isNewEntry: isNewEntry,
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

	for r.scanner.Scan() {
		if stopped || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		lineNum++
		line := r.scanner.Text()

		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}

		if r.isNewEntry(line) {
			flush()
			current = &strings.Builder{}
			current.WriteString(line)
			currentLineNum = lineNum
		} else if current != nil {
			current.WriteByte('\n')
			current.WriteString(line)
		} else {
			current = &strings.Builder{}
			current.WriteString(line)
			currentLineNum = lineNum
			flush()
		}
	}

	flush()
	return r.scanner.Err()
}

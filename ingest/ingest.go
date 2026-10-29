package ingest

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/oxforge/unlog/types"
)

var logExtensions = map[string]bool{
	".log":    true,
	".txt":    true,
	".json":   true,
	".csv":    true,
	".out":    true,
	".jsonl":  true,
	".ndjson": true,
	"":        true, // extensionless files (e.g. /var/log/syslog, /var/log/messages)
}

var compressedExtensions = map[string]bool{
	".gz":  true,
	".tgz": true,
}

// IngestOptions controls Ingester behaviour.
type IngestOptions struct {
	// SampleLines is how many lines to read for format detection. Default: 100.
	SampleLines int
	// Location is the zone of timestamps that carry none. Default: UTC.
	Location *time.Location
}

func (o IngestOptions) withDefaults() IngestOptions {
	if o.SampleLines <= 0 {
		o.SampleLines = 100
	}
	return o
}

// Ingester reads log sources and emits LogEntry values on its output channel.
type Ingester struct {
	output chan<- types.LogEntry
	opts   IngestOptions
	stats  *statsCollector
}

// NewIngester creates an Ingester that writes parsed entries to output.
func NewIngester(output chan<- types.LogEntry, opts IngestOptions) *Ingester {
	return &Ingester{
		output: output,
		opts:   opts.withDefaults(),
		stats:  newStatsCollector(),
	}
}

// SourceStats returns per-source ingestion statistics. Call after Run completes.
func (ing *Ingester) SourceStats() map[string]SourceStats {
	return ing.stats.Results()
}

// Run starts ingestion of the given sources. It closes the output channel when
// all sources have been processed. A "-" source, or no sources at all, reads
// os.Stdin. Each source is processed in its own goroutine. A missing path, or
// no log file found among the sources, is an error; binary files (a NUL byte
// in their first 8 KB, like /var/log/wtmp) are skipped with a warning.
//
// Note: when multiple files are processed concurrently, entries from different
// files are interleaved in arbitrary order on the output channel. Downstream
// stages (filter) sort survivors by timestamp. In overflow mode, ordering is
// not guaranteed.
func (ing *Ingester) Run(ctx context.Context, sources []string) error {
	defer close(ing.output)

	readStdin := len(sources) == 0
	var paths []string
	for _, src := range sources {
		if src == "-" {
			readStdin = true
		} else {
			paths = append(paths, src)
		}
	}

	files, err := ExpandSources(paths)
	if err != nil {
		return err
	}
	if len(files) == 0 && !readStdin {
		return fmt.Errorf("ingest: no log files found in %s", strings.Join(paths, ", "))
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	if readStdin {
		g.Go(func() error {
			return ing.processSource(gCtx, "stdin", os.Stdin, time.Now())
		})
	}

	for _, path := range files {
		g.Go(func() error {
			f, err := os.Open(path)
			if err != nil {
				slog.Warn("ingest: skipping file", "path", path, "error", err)
				return nil
			}
			defer func() { _ = f.Close() }()

			if !isTarGz(path) && !isPlainGz(path) && isBinary(f) {
				slog.Warn("ingest: skipping binary file", "path", path)
				return nil
			}

			modTime := time.Now()
			if info, err := f.Stat(); err == nil {
				modTime = info.ModTime()
			}
			if err := ing.processFile(gCtx, path, f, modTime); err != nil {
				if gCtx.Err() != nil {
					return gCtx.Err()
				}
				slog.Warn("ingest: error processing file", "path", path, "error", err)
			}
			return nil
		})
	}

	return g.Wait()
}

// processSource detects the format of r, then parses and emits all entries.
// modTime is when the source was last written; it dates year-less timestamps.
func (ing *Ingester) processSource(ctx context.Context, source string, r io.Reader, modTime time.Time) error {
	detectionLines, rawSample, remainder, err := ing.sampleInput(r)
	if err != nil {
		return err
	}

	format := formatFromExtension(source)
	if format == FormatUnknown {
		format = detectFormat(detectionLines)
	}
	parser := parserForFormat(format, modTime, ing.opts.Location)
	checker := lineCheckerForFormat(format)

	slog.Debug("ingest: detected format", "source", source, "format", format.String())
	ing.stats.register(source, format.String())

	combined := io.MultiReader(strings.NewReader(rawSample), remainder)
	rd := newReader(combined, source, checker)

	var sendErr error
	readErr := rd.ReadFuncContext(ctx, func(raw RawLine) bool {
		entry, unparsed, keep := parseOrFallback(parser, raw)
		if !keep {
			return true
		}
		if entry.Level == types.LevelUnknown && entry.Message != "" {
			entry.Level = InferLevel(entry.Message)
		}
		if entry.Service == "" {
			entry.Service = serviceOf(entry.Metadata)
		}
		ing.stats.record(source, entry.Level, unparsed)
		select {
		case ing.output <- entry:
			return true
		case <-ctx.Done():
			sendErr = ctx.Err()
			return false
		}
	})
	if sendErr != nil {
		return sendErr
	}
	return readErr
}

// serviceKeys are the metadata fields, in order of preference, that name the
// service a line came from: a JSON or logfmt "service", a syslog program
// ("app"), a Docker Compose "container", a Kubernetes "pod".
var serviceKeys = []string{"service", "app", "container", "pod"}

func serviceOf(metadata map[string]string) string {
	for _, key := range serviceKeys {
		if v := metadata[key]; v != "" {
			return v
		}
	}
	return ""
}

// isBinary reports whether f starts with a NUL byte in its first 8 KB, and
// rewinds it.
func isBinary(f io.ReadSeeker) bool {
	buf := make([]byte, 8*1024)
	n, _ := io.ReadFull(f, buf)
	_, _ = f.Seek(0, io.SeekStart)
	return bytes.IndexByte(buf[:n], 0) >= 0
}

// headerSkipper is implemented by parsers that consume a line without
// producing an entry on purpose (the CSV header), so it is not a rejection.
type headerSkipper interface {
	consumedHeader() bool
}

// parseOrFallback parses a reassembled line with the detected format's
// parser, so that no input is lost when the parser rejects it:
//   - a multi-line group the parser rejects as a whole (a JSON line followed
//     by a plain-text traceback) is parsed by its first line, and the other
//     lines are appended to the message, as other formats keep stack traces;
//   - anything else the parser rejects becomes a raw entry, and unparsed is
//     true.
//
// keep is false only for a line the parser skipped on purpose.
func parseOrFallback(parser Parser, raw RawLine) (entry types.LogEntry, unparsed, keep bool) {
	if entry, ok := parser.Parse(raw.Text, raw.LineNumber, raw.Source); ok {
		return entry, false, true
	}
	if hs, ok := parser.(headerSkipper); ok && hs.consumedHeader() {
		return types.LogEntry{}, false, false
	}
	if first, rest, multiLine := strings.Cut(raw.Text, "\n"); multiLine {
		if entry, ok := parser.Parse(first, raw.LineNumber, raw.Source); ok {
			if entry.Message == "" {
				entry.Message = rest
			} else {
				entry.Message += "\n" + rest
			}
			entry.RawLine = raw.Text
			return entry, false, true
		}
	}
	entry, _ = (&rawParser{}).Parse(raw.Text, raw.LineNumber, raw.Source)
	return entry, true, true
}

// sampleInput reads up to SampleLines lines from r and returns them along with
// the raw bytes consumed and an io.Reader for the remainder of the stream.
func (ing *Ingester) sampleInput(r io.Reader) ([]string, string, io.Reader, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var detectionLines []string
	var rawSample strings.Builder

	for i := 0; i < ing.opts.SampleLines; i++ {
		line, err := br.ReadString('\n')
		rawSample.WriteString(line)
		trimmed := strings.TrimRight(line, "\n\r")
		if trimmed != "" {
			detectionLines = append(detectionLines, trimmed)
		}
		if err == io.EOF {
			return detectionLines, rawSample.String(), strings.NewReader(""), nil
		}
		if err != nil {
			return detectionLines, rawSample.String(), strings.NewReader(""), err
		}
	}

	return detectionLines, rawSample.String(), br, nil
}

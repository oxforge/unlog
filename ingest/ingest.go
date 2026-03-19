package ingest

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/oxforge/unlog/types"
)

var logExtensions = map[string]bool{
	".log":  true,
	".txt":  true,
	".json": true,
	".csv":  true,
	".out":  true,
	"":      true, // extensionless files (e.g. /var/log/syslog, /var/log/messages)
}

var compressedExtensions = map[string]bool{
	".gz":  true,
	".tgz": true,
}

// IngestOptions controls Ingester behaviour.
type IngestOptions struct {
	// BufferSize is the output channel buffer size. Default: 100_000.
	BufferSize int
	// SampleLines is how many lines to read for format detection. Default: 100.
	SampleLines int
}

func (o IngestOptions) withDefaults() IngestOptions {
	if o.BufferSize <= 0 {
		o.BufferSize = 100_000
	}
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
// all sources have been processed. If sources is empty or contains only "-",
// os.Stdin is read instead. Each regular file source is processed in its own
// goroutine.
//
// Note: when multiple files are processed concurrently, entries from different
// files are interleaved in arbitrary order on the output channel. Downstream
// stages (filter) sort survivors by timestamp. In overflow mode, ordering is
// not guaranteed.
func (ing *Ingester) Run(ctx context.Context, sources []string) error {
	defer close(ing.output)

	if len(sources) == 0 || (len(sources) == 1 && sources[0] == "-") {
		return ing.processSource(ctx, "stdin", os.Stdin)
	}

	files, err := ExpandSources(sources)
	if err != nil {
		return err
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	for _, path := range files {
		g.Go(func() error {
			f, err := os.Open(path)
			if err != nil {
				slog.Warn("ingest: skipping file", "path", path, "error", err)
				return nil
			}
			defer func() { _ = f.Close() }()

			if err := ing.processFile(gCtx, path, f); err != nil {
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
func (ing *Ingester) processSource(ctx context.Context, source string, r io.Reader) error {
	detectionLines, rawSample, remainder, err := ing.sampleInput(r)
	if err != nil {
		return err
	}

	format := formatFromExtension(source)
	if format == FormatUnknown {
		format = detectFormat(detectionLines)
	}
	parser := parserForFormat(format)
	checker := lineCheckerForFormat(format)

	slog.Debug("ingest: detected format", "source", source, "format", format.String())
	ing.stats.register(source, format.String())

	combined := io.MultiReader(strings.NewReader(rawSample), remainder)
	rd := newReader(combined, source, checker)

	var sendErr error
	readErr := rd.ReadFuncContext(ctx, func(raw RawLine) bool {
		entry, ok := parser.Parse(raw.Text, raw.LineNumber, raw.Source)
		if !ok {
			return true
		}
		if entry.Level == types.LevelUnknown && entry.Message != "" {
			entry.Level = InferLevel(entry.Message)
		}
		ing.stats.record(source, entry.Level)
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

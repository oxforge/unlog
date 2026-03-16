package ingest

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/oxforge/unlog/types"
)

var logExtensions = map[string]bool{
	".log":  true,
	".txt":  true,
	".json": true,
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
}

// NewIngester creates an Ingester that writes parsed entries to output.
func NewIngester(output chan<- types.LogEntry, opts IngestOptions) *Ingester {
	return &Ingester{
		output: output,
		opts:   opts.withDefaults(),
	}
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

	files, err := ing.expandSources(sources)
	if err != nil {
		return err
	}

	g, gCtx := errgroup.WithContext(ctx)

	for _, file := range files {
		path := file
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

// processFile routes to the appropriate handler based on file extension.
func (ing *Ingester) processFile(ctx context.Context, path string, f *os.File) error {
	if isTarGz(path) {
		return ing.processTarGz(ctx, path, f)
	}
	if isPlainGz(path) {
		return ing.processGz(ctx, path, f)
	}
	return ing.processSource(ctx, path, f)
}

// processGz decompresses a .gz file and processes the inner stream as a single source.
// The source name strips the .gz suffix (e.g. "app.log.gz" → "app.log.gz:app.log").
func (ing *Ingester) processGz(ctx context.Context, path string, r io.Reader) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	inner := strings.TrimSuffix(filepath.Base(path), ".gz")
	source := path + ":" + inner
	return ing.processSource(ctx, source, gr)
}

// processTarGz decompresses a .tar.gz/.tgz archive and processes each log file entry.
func (ing *Ingester) processTarGz(ctx context.Context, path string, r io.Reader) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(hdr.Name)
		if strings.HasPrefix(base, "._") || strings.HasPrefix(base, ".") {
			continue
		}
		if !isLogName(hdr.Name) {
			slog.Debug("ingest: skipping non-log tar entry", "archive", path, "entry", hdr.Name)
			continue
		}
		source := path + ":" + hdr.Name
		if err := ing.processSource(ctx, source, tr); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("ingest: error processing tar entry", "archive", path, "entry", hdr.Name, "error", err)
		}
	}
}

// processSource detects the format of r, then parses and emits all entries.
func (ing *Ingester) processSource(ctx context.Context, source string, r io.Reader) error {
	detectionLines, rawSample, remainder, err := ing.sampleInput(r)
	if err != nil {
		return err
	}

	format := detectFormat(detectionLines)
	parser := parserForFormat(format)
	checker := lineCheckerForFormat(format)

	slog.Debug("ingest: detected format", "source", source, "format", format.String())

	combined := io.MultiReader(strings.NewReader(rawSample), remainder)
	rd := newReader(combined, source, checker)

	var sendErr error
	readErr := rd.ReadFuncContext(ctx, func(raw RawLine) bool {
		entry, ok := parser.Parse(raw.Text, raw.LineNumber, raw.Source)
		if !ok {
			return true
		}
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

// isTarGz returns true if the path is a .tar.gz or .tgz archive.
func isTarGz(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
}

// isPlainGz returns true if the path is a .gz file that is NOT a .tar.gz.
func isPlainGz(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".gz") && !strings.HasSuffix(lower, ".tar.gz")
}

// isLogName reports whether name (a tar entry or inner filename) has a
// recognised log extension.
func isLogName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return logExtensions[ext]
}

func (ing *Ingester) expandSources(sources []string) ([]string, error) {
	return ExpandSources(sources)
}

// ExpandSources resolves globs and walks directories, returning a deduplicated
// list of regular file paths with recognised log extensions (.log, .txt, .json,
// .out, extensionless) and compressed archives (.gz, .tgz, .tar.gz).
func ExpandSources(sources []string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	for _, src := range sources {
		matches, err := filepath.Glob(src)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			// Treat as a literal path even if the glob matched nothing.
			matches = []string{src}
		}

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				slog.Warn("ingest: skipping", "path", match, "error", err)
				continue
			}

			if info.IsDir() {
				dirFiles, err := walkLogDir(match)
				if err != nil {
					slog.Warn("ingest: error walking directory", "path", match, "error", err)
					continue
				}
				for _, f := range dirFiles {
					if !seen[f] {
						seen[f] = true
						files = append(files, f)
					}
				}
			} else {
				if !seen[match] {
					seen[match] = true
					files = append(files, match)
				}
			}
		}
	}

	return files, nil
}

// walkLogDir returns all log and compressed archive files found recursively under dir.
func walkLogDir(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if compressedExtensions[ext] || logExtensions[ext] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

package ingest

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
)

// processFile routes to the appropriate handler based on file extension.
func (ing *Ingester) processFile(ctx context.Context, path string, f io.ReadSeeker) error {
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

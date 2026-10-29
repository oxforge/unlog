package ingest

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// reRotationSuffix matches a rotation suffix: "app.log.1", "app.log.2024-01-15",
// "app.log-20240115". Group 1 is the name before it.
var reRotationSuffix = regexp.MustCompile(`^(.+?)[.-]\d+(?:-\d+)*$`)

// ExpandSources resolves globs and walks directories, returning a deduplicated
// list of regular file paths: files named explicitly, and inside directories
// the log files (see isLogName) and compressed archives (.gz, .tgz, .tar.gz),
// skipping hidden files and directories. A path that does not exist, or a
// glob that matches nothing, is an error.
func ExpandSources(sources []string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	for _, src := range sources {
		matches, err := filepath.Glob(src)
		if err != nil {
			return nil, fmt.Errorf("ingest: %w", err)
		}
		if len(matches) == 0 {
			// Treat as a literal path even if the glob matched nothing.
			matches = []string{src}
		}

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, fmt.Errorf("ingest: %w", err)
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
			} else if !seen[match] {
				seen[match] = true
				files = append(files, match)
			}
		}
	}

	return files, nil
}

// walkLogDir returns all log and compressed archive files found recursively
// under dir, skipping hidden files and directories.
func walkLogDir(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("ingest: skipping path", "path", path, "error", err)
			return nil
		}
		if path != dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if compressedExtensions[strings.ToLower(filepath.Ext(path))] || isLogName(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// isLogName reports whether name (a path, tar entry or inner filename) looks
// like a log file: a log extension or none, possibly followed by rotation
// suffixes ("app.log.1", "syslog.2", "app.log-20240115"). Hidden names are not.
func isLogName(name string) bool {
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".") {
		return false
	}
	for {
		if logExtensions[strings.ToLower(filepath.Ext(base))] {
			return true
		}
		m := reRotationSuffix.FindStringSubmatch(base)
		if m == nil {
			return false
		}
		base = m[1]
	}
}

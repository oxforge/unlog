package ingest

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

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
			slog.Warn("ingest: skipping path", "path", path, "error", err)
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

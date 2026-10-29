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
// the log files (see isLogName) and compressed logs and archives (.gz of a
// log name, .tgz, .tar.gz), skipping hidden files and directories and not
// following symlinked directories. A path that does not exist, or a glob that
// matches nothing, is an error.
func ExpandSources(sources []string) ([]string, error) {
	found, err := expandSources(sources)
	if err != nil {
		return nil, err
	}
	files := make([]string, len(found))
	for i, f := range found {
		files[i] = f.path
	}
	return files, nil
}

// source is a file to read, and whether it was named (directly or by a glob)
// rather than found by walking a directory.
type source struct {
	path  string
	named bool
}

func expandSources(args []string) ([]source, error) {
	var found []source
	index := make(map[string]int) // cleaned path → its position in found
	add := func(path string, named bool) {
		path = filepath.Clean(path)
		if i, ok := index[path]; ok {
			found[i].named = found[i].named || named
			return
		}
		index[path] = len(found)
		found = append(found, source{path: path, named: named})
	}

	for _, arg := range args {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return nil, fmt.Errorf("ingest: %w", err)
		}
		if len(matches) == 0 {
			// Treat as a literal path even if the glob matched nothing.
			matches = []string{arg}
		}

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, fmt.Errorf("ingest: %w", err)
			}
			if !info.IsDir() {
				add(match, true)
				continue
			}
			dirFiles, err := walkLogDir(match)
			if err != nil {
				slog.Warn("ingest: error walking directory", "path", match, "error", err)
				continue
			}
			for _, f := range dirFiles {
				add(f, false)
			}
		}
	}

	return found, nil
}

// walkLogDir returns all log and compressed files found recursively under
// dir, skipping hidden files and directories. A symlinked dir is walked
// under the name it was given; symlinks to directories inside it are not
// followed, so a link back up can't loop.
func walkLogDir(dir string) ([]string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("ingest: skipping path", "path", path, "error", err)
			return nil
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				slog.Debug("ingest: not following symlink", "path", path)
				return nil
			}
		}
		if isLogName(path) || isCompressedLog(path) {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.Join(dir, rel))
		}
		return nil
	})
	return files, err
}

// isCompressedLog reports whether name is a .tgz/.tar.gz archive (whose
// entries are filtered later) or a .gz of a log file ("app.log.3.gz", not
// "backup.sql.gz").
func isCompressedLog(name string) bool {
	if isTarGz(name) {
		return true
	}
	return isPlainGz(name) && isLogName(name[:len(name)-len(".gz")])
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

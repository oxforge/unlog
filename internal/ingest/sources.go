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
// log name, .tgz, .tar.gz), skipping hidden files and directories. A file
// reached by two spellings or through a symlink is listed once, under the
// first. A path that does not exist, or a glob that matches nothing, is an
// error.
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

// source is a file to read. named is set for a file given on the command
// line; a file searched for, by walking a directory or by a glob unlog
// expands itself, has the directory or glob it came from in from.
type source struct {
	path  string
	named bool
	from  string
}

func expandSources(args []string) ([]source, error) {
	var found []source
	index := make(map[string]int) // the file's resolved path → its position in found
	add := func(path string, named bool, from string) {
		path = filepath.Clean(path)
		key := resolvedPath(path)
		if i, ok := index[key]; ok {
			if named {
				found[i].named, found[i].from = true, ""
			}
			return
		}
		index[key] = len(found)
		found = append(found, source{path: path, named: named, from: from})
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
		// What a glob unlog expands itself finds is searched for, like a
		// directory's files; a shell's glob arrives as names.
		searched := len(matches) > 0 && strings.ContainsAny(arg, "*?[") && matches[0] != arg

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, fmt.Errorf("ingest: %w", err)
			}
			if !info.IsDir() {
				if searched {
					add(match, false, arg)
				} else {
					add(match, true, "")
				}
				continue
			}
			dirFiles, err := walkLogDir(match)
			if err != nil {
				slog.Warn("ingest: error walking directory", "path", match, "error", err)
				continue
			}
			for _, f := range dirFiles {
				add(f, false, match)
			}
		}
	}

	return found, nil
}

// resolvedPath is path made absolute with its symlinks resolved, so two
// spellings of one file compare equal; path itself when that fails.
func resolvedPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// walkLogDir returns all log and compressed files found recursively under
// dir, skipping hidden files and directories, each under the name it was
// reached by. A symlink to a directory outside the trees already searched
// is followed once; one into them, or to one of their ancestors, is not, so
// nothing is read twice and no link loops.
func walkLogDir(dir string) ([]string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	sep := string(filepath.Separator)
	searched := []string{root}
	covered := func(real string) bool {
		for _, w := range searched {
			if real == w || strings.HasPrefix(real, w+sep) || strings.HasPrefix(w, real+sep) {
				return true
			}
		}
		return false
	}

	var files []string
	var walk func(real, shown string) error
	walk = func(real, shown string) error {
		return filepath.WalkDir(real, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				slog.Warn("ingest: skipping path", "path", path, "error", err)
				return nil
			}
			if path != real && strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(real, path)
			if err != nil {
				return err
			}
			name := filepath.Join(shown, rel)
			if d.Type()&fs.ModeSymlink != 0 {
				target, err := filepath.EvalSymlinks(path)
				if err != nil {
					slog.Debug("ingest: skipping broken symlink", "path", name)
					return nil
				}
				if info, err := os.Stat(target); err == nil && info.IsDir() {
					if covered(target) {
						slog.Debug("ingest: not following symlink into a searched tree", "path", name)
						return nil
					}
					searched = append(searched, target)
					return walk(target, name)
				}
			}
			if isLogName(path) || isCompressedLog(path) {
				files = append(files, name)
			}
			return nil
		})
	}
	return files, walk(root, dir)
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

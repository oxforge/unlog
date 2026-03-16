package filter

import (
	"bufio"
	"os"
	"strings"

	"github.com/oxforge/unlog/noise"
	"github.com/oxforge/unlog/types"
)

// NoiseFilter drops log entries matching known noise patterns.
type NoiseFilter struct {
	patterns []string
}

// NewNoiseFilter creates a NoiseFilter loaded with built-in patterns
// and optionally a custom noise file.
func NewNoiseFilter(customFile string) (*NoiseFilter, error) {
	// Load built-in patterns via go:embed (always available in binary).
	patterns := scanPatternsFromString(noise.DefaultPatterns)

	// Load custom patterns if specified.
	if customFile != "" {
		custom, err := loadPatternsFromFile(customFile)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, custom...)
	}

	// Lowercase all patterns for case-insensitive matching.
	lower := make([]string, len(patterns))
	for i, p := range patterns {
		lower[i] = strings.ToLower(p)
	}

	return &NoiseFilter{patterns: lower}, nil
}

func scanPatternsFromString(text string) []string {
	return scanPatterns(bufio.NewScanner(strings.NewReader(text)))
}

func loadPatternsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file
	patterns := scanPatterns(bufio.NewScanner(f))
	return patterns, nil
}

func scanPatterns(s *bufio.Scanner) []string {
	var patterns []string
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func (f *NoiseFilter) Filter(entry types.LogEntry) bool {
	if entry.Message == "" {
		return true
	}
	lower := strings.ToLower(entry.Message)
	for _, p := range f.patterns {
		if strings.Contains(lower, p) {
			return false
		}
	}
	return true
}

func (f *NoiseFilter) Name() string {
	return "noise"
}

package ingest

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reKubernetes = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s+(stdout|stderr)\s+[FP]\s+`)
	reSyslog5424 = regexp.MustCompile(`^<\d+>\d+\s+`)
	reSyslog3164 = regexp.MustCompile(`^<\d+>(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s`)
	reCLF        = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\[\d{2}/[A-Z]`)
	reLogfmtPair = regexp.MustCompile(`\b\w+=(?:"[^"]*"|\S+)`)
	reKubePrefixed  = regexp.MustCompile(`^\[[\w/.:-]+\]\s+\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reDockerCompose = regexp.MustCompile(`^\w[\w.-]*-\d+\s+\|\s+`)
	reGenericTS    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reCSVData    = regexp.MustCompile(`(?i)^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}[^,]*,\s*(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL|ERR)\s*,`)
)

// classifyLine returns the most specific Format that matches line.
func classifyLine(line string) Format {
	line = strings.TrimSpace(line)
	if line == "" {
		return FormatUnknown
	}

	if line[0] == '{' {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(line), &obj) == nil {
			_, hasLog := obj["log"]
			_, hasStream := obj["stream"]
			_, hasTime := obj["time"]
			if hasLog && hasStream && hasTime {
				return FormatDockerJSON
			}

			_, hasAtTS := obj["@timestamp"]
			_, hasAtMsg := obj["@message"]
			if hasAtTS && hasAtMsg {
				return FormatCloudWatch
			}

			_, hasLevel := obj["level"]
			_, hasSeverity := obj["severity"]
			_, hasMsg := obj["msg"]
			_, hasMessage := obj["message"]
			if hasLevel || hasSeverity || hasMsg || hasMessage {
				return FormatJSON
			}
		}
	}

	if reKubernetes.MatchString(line) || reKubePrefixed.MatchString(line) {
		return FormatKubernetes
	}

	if reSyslog5424.MatchString(line) {
		return FormatSyslog5424
	}

	if reSyslog3164.MatchString(line) {
		return FormatSyslog3164
	}

	if reCLF.MatchString(line) {
		return FormatCLF
	}

	if isCSVHeaderLine(line) || reCSVData.MatchString(line) {
		return FormatCSV
	}

	if matches := reLogfmtPair.FindAllString(line, -1); len(matches) >= 3 {
		return FormatLogfmt
	}

	if reDockerCompose.MatchString(line) {
		return FormatDockerCompose
	}

	if reGenericTS.MatchString(line) {
		return FormatGeneric
	}

	return FormatRaw
}

// isCSVHeaderLine parses line as a quoted CSV row and checks whether it looks
// like a header using the same knownHeaders set as the CSV parser.
func isCSVHeaderLine(line string) bool {
	fields, ok := parseCSVLine(line)
	if !ok {
		return false
	}
	return isHeaderRow(fields)
}

// formatFromExtension returns a format hint based on the file extension of source.
// Returns FormatUnknown if the extension doesn't map to a specific format.
// Note: .json is intentionally excluded — JSON is an encoding shared by multiple
// formats (structured JSON, Docker JSON, CloudWatch), and content-based detection
// is needed to distinguish them.
func formatFromExtension(source string) Format {
	// Strip archive qualifiers like "file.tar.gz:inner.csv" → use inner name.
	if idx := strings.LastIndex(source, ":"); idx >= 0 {
		source = source[idx+1:]
	}
	ext := strings.ToLower(filepath.Ext(source))
	switch ext {
	case ".csv":
		return FormatCSV
	default:
		return FormatUnknown
	}
}

// DetectFormat inspects a sample of lines and returns the most likely Format
// using majority vote (>60% threshold).
func DetectFormat(lines []string) Format {
	return detectFormat(lines)
}

func detectFormat(lines []string) Format {
	if len(lines) == 0 {
		return FormatRaw
	}

	votes := make(map[Format]int)
	for _, line := range lines {
		f := classifyLine(line)
		if f != FormatUnknown && f != FormatRaw {
			votes[f]++
		}
	}

	var bestFormat Format
	var bestCount int
	for f, count := range votes {
		if count > bestCount {
			bestFormat = f
			bestCount = count
		}
	}

	total := 0
	for _, count := range votes {
		total += count
	}
	if total > 0 && float64(bestCount)/float64(total) > 0.6 {
		return bestFormat
	}

	// A CSV header is a strong signal — even a single header row among
	// otherwise-unclassifiable data lines is enough to select CSV.
	if votes[FormatCSV] > 0 {
		return FormatCSV
	}
	if votes[FormatGeneric] > 0 {
		return FormatGeneric
	}
	return FormatRaw
}

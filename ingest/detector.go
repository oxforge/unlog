package ingest

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oxforge/unlog/types"
)

var (
	reKubernetes = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s+(stdout|stderr)\s+[FP]\s+`)
	reSyslog5424 = regexp.MustCompile(`^<\d+>\d+\s+`)
	// reSyslog3164 matches BSD syslog: "<34>Jan 15 10:00:00 host" on the wire,
	// "Jan 15 10:00:00 host" in files rsyslog writes (no priority).
	reSyslog3164 = regexp.MustCompile(`^(?:<\d+>)?(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\S+\s`)
	// reSyslogISO matches rsyslog's high-precision file format: an RFC 3339
	// timestamp with microseconds and a numeric offset, then host and tag.
	// Group 1 is the host.
	reSyslogISO  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[+-]\d{2}:\d{2}\s+(\S+)\s+[^\s\[:]+(?:\[\d+\])?:\s`)
	reCLF        = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\[\d{2}/[A-Z]`)
	reLogfmtPair = regexp.MustCompile(`\b\w+=(?:"[^"]*"|\S+)`)
	// reLogfmtKey matches a key=value pair at the start of a line.
	reLogfmtKey     = regexp.MustCompile(`^\w[\w.-]*=`)
	reKubePrefixed  = regexp.MustCompile(`^\[[\w/.:-]+\]\s+\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reDockerCompose = regexp.MustCompile(`^\w[\w.-]*-\d+\s+\|\s+`)
	reGenericTS     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reCSVData       = regexp.MustCompile(`(?i)^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}[^,]*,\s*(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL|ERR)\s*,`)
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

	if isSyslog3164Line(line) {
		return FormatSyslog3164
	}

	if reCLF.MatchString(line) {
		return FormatCLF
	}

	if isCSVHeaderLine(line) || reCSVData.MatchString(line) {
		return FormatCSV
	}

	if isLogfmtLine(line) {
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

// isSyslog3164Line reports whether line is BSD-style syslog, with a
// month-day timestamp or rsyslog's high-precision one. A "host" that is a level
// keyword ("2024-... ERROR db: lost") marks an application log, not syslog.
func isSyslog3164Line(line string) bool {
	if reSyslog3164.MatchString(line) {
		return true
	}
	m := reSyslogISO.FindStringSubmatch(line)
	return m != nil && types.ParseLevel(m[1]) == types.LevelUnknown
}

// isLogfmtLine reports whether line is logfmt: it starts with a key=value
// pair and has at least two. Pairs after free text ("2024-01-15 10:00:01 ERROR
// payment failed user=42") do not count; that line is generic text whose
// trailing pairs the generic parser lifts into metadata.
func isLogfmtLine(line string) bool {
	return reLogfmtKey.MatchString(line) && len(reLogfmtPair.FindAllString(line, 2)) == 2
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

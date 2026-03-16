package ingest

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	reKubernetes = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s+(stdout|stderr)\s+[FP]\s+`)
	reSyslog5424 = regexp.MustCompile(`^<\d+>\d+\s+`)
	reSyslog3164 = regexp.MustCompile(`^<\d+>(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s`)
	reCLF        = regexp.MustCompile(`^\S+\s+\S+\s+\S+\s+\[`)
	reLogfmtPair = regexp.MustCompile(`\b\w+=(?:"[^"]*"|\S+)`)
	reGenericTS  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
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

	if reKubernetes.MatchString(line) {
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

	if matches := reLogfmtPair.FindAllString(line, -1); len(matches) >= 3 {
		return FormatLogfmt
	}

	if reGenericTS.MatchString(line) {
		return FormatGeneric
	}

	return FormatRaw
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
		if f != FormatUnknown {
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

	if votes[FormatGeneric] > 0 {
		return FormatGeneric
	}
	return FormatRaw
}

// parserForFormat returns the appropriate Parser for a detected format.
func parserForFormat(format Format) Parser {
	switch format {
	case FormatDockerJSON:
		return &dockerParser{}
	case FormatCloudWatch:
		return &cloudwatchParser{}
	case FormatKubernetes:
		return &kubeParser{}
	case FormatJSON:
		return &jsonParser{}
	case FormatSyslog5424:
		return newSyslogParser(true)
	case FormatSyslog3164:
		return newSyslogParser(false)
	case FormatCLF:
		return &clfParser{}
	case FormatLogfmt:
		return &logfmtParser{}
	case FormatGeneric:
		return &genericParser{}
	default:
		return &rawParser{}
	}
}

// lineCheckerForFormat returns a LineChecker that recognises the first line of
// a new log entry for the given format. This is used by the reader to reassemble
// multi-line entries (e.g. stack traces).
func lineCheckerForFormat(format Format) LineChecker {
	switch format {
	case FormatDockerJSON, FormatCloudWatch, FormatJSON:
		return func(line string) bool {
			return len(line) > 0 && line[0] == '{'
		}
	case FormatKubernetes:
		return func(line string) bool {
			return reKubernetes.MatchString(line)
		}
	case FormatSyslog5424:
		return func(line string) bool {
			return reSyslog5424.MatchString(line)
		}
	case FormatSyslog3164:
		return func(line string) bool {
			return reSyslog3164.MatchString(line)
		}
	case FormatCLF:
		return func(line string) bool {
			return reCLF.MatchString(line)
		}
	case FormatLogfmt:
		return func(line string) bool {
			return len(reLogfmtPair.FindAllString(line, 3)) >= 3
		}
	case FormatGeneric:
		return func(line string) bool {
			return reGenericTS.MatchString(line)
		}
	default:
		return func(line string) bool {
			return true
		}
	}
}

package ingest

import (
	"strings"

	"github.com/oxforge/unlog/types"
)

// Format identifies a log format.
type Format int

const (
	FormatUnknown Format = iota
	FormatDockerJSON
	FormatDockerCompose
	FormatCloudWatch
	FormatKubernetes
	FormatJSON
	FormatSyslog5424
	FormatSyslog3164
	FormatCLF
	FormatLogfmt
	FormatCSV
	FormatGeneric
	FormatRaw
)

func (f Format) String() string {
	switch f {
	case FormatDockerJSON:
		return "docker-json"
	case FormatDockerCompose:
		return "docker-compose"
	case FormatCloudWatch:
		return "cloudwatch"
	case FormatKubernetes:
		return "kubernetes"
	case FormatJSON:
		return "json"
	case FormatSyslog5424:
		return "syslog-rfc5424"
	case FormatSyslog3164:
		return "syslog-rfc3164"
	case FormatCLF:
		return "clf"
	case FormatLogfmt:
		return "logfmt"
	case FormatCSV:
		return "csv"
	case FormatGeneric:
		return "generic"
	case FormatRaw:
		return "raw"
	default:
		return "unknown"
	}
}

// Parser extracts a LogEntry from a single logical line.
// Implementations are not required to be goroutine-safe; callers must
// create one Parser per source (this is already the pattern in processSource).
type Parser interface {
	Parse(line string, lineNum int64, source string) (types.LogEntry, bool)
	Name() string
}

// parserForFormat returns the appropriate Parser for a detected format.
func parserForFormat(format Format) Parser {
	switch format {
	case FormatDockerJSON:
		return &dockerParser{}
	case FormatDockerCompose:
		return &dockerComposeParser{}
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
	case FormatCSV:
		return &csvParser{}
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
	case FormatDockerCompose:
		return func(line string) bool {
			if idx := strings.Index(line, " | "); idx >= 0 {
				return reGenericTS.MatchString(strings.TrimSpace(line[idx+3:]))
			}
			return false
		}
	case FormatKubernetes:
		return func(line string) bool {
			return reKubernetes.MatchString(line) || reKubePrefixed.MatchString(line)
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
	case FormatCSV:
		return func(line string) bool {
			return len(line) > 0
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

package ingest

import (
	"strings"
	"time"

	"github.com/oxforge/unlog/internal/types"
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
}

// parserForFormat returns the appropriate Parser for a detected format. ref
// is when the source was last written (its mtime), which dates timestamps that
// carry no year; loc is the zone of timestamps that carry none (nil = UTC).
func parserForFormat(format Format, ref time.Time, loc *time.Location) Parser {
	ts := formatCache{loc: loc, ref: ref}
	switch format {
	case FormatDockerJSON:
		return &dockerParser{tsCache: ts}
	case FormatDockerCompose:
		return &dockerComposeParser{tsCache: ts}
	case FormatCloudWatch:
		return &cloudwatchParser{tsCache: ts}
	case FormatKubernetes:
		return &kubeParser{tsCache: ts}
	case FormatJSON:
		return &jsonParser{tsCache: ts}
	case FormatSyslog5424:
		p := newSyslogParser(true, ref)
		p.tsCache = ts
		return p
	case FormatSyslog3164:
		p := newSyslogParser(false, ref)
		p.tsCache = ts
		return p
	case FormatCLF:
		return &clfParser{tsCache: ts}
	case FormatLogfmt:
		return &logfmtParser{tsCache: ts}
	case FormatCSV:
		return &csvParser{tsCache: ts}
	case FormatGeneric:
		return &genericParser{tsCache: ts}
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
		return isSyslog3164Line
	case FormatCLF:
		return func(line string) bool {
			return reCLF.MatchString(line)
		}
	case FormatLogfmt:
		return isLogfmtLine
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

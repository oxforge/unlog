package ingest

import "github.com/oxforge/unlog/types"

// Format identifies a log format.
type Format int

const (
	FormatUnknown Format = iota
	FormatDockerJSON
	FormatCloudWatch
	FormatKubernetes
	FormatJSON
	FormatSyslog5424
	FormatSyslog3164
	FormatCLF
	FormatLogfmt
	FormatGeneric
	FormatRaw
)

func (f Format) String() string {
	switch f {
	case FormatDockerJSON:
		return "docker-json"
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
	case FormatGeneric:
		return "generic"
	case FormatRaw:
		return "raw"
	default:
		return "unknown"
	}
}

// Parser extracts a LogEntry from a single logical line.
type Parser interface {
	Parse(line string, lineNum int64, source string) (types.LogEntry, bool)
	Name() string
}

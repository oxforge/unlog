package ingest

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oxforge/unlog/internal/types"
)

var (
	// reSyslog3164Full matches BSD syslog with or without the priority.
	// Groups: priority (optional), month, day, time, hostname, app, pid, message.
	reSyslog3164Full = regexp.MustCompile(`^(?:<(\d+)>)?(\w{3})\s+(\d{1,2})\s+(\d{2}:\d{2}:\d{2})\s+(\S+)\s+([^\s\[:]+)(?:\[(\d+)])?:\s*(.*)$`)
	// reSyslogISOFull matches rsyslog's high-precision file format.
	// Groups: timestamp, hostname, app, pid, message.
	reSyslogISOFull = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+[+-]\d{2}:\d{2})\s+(\S+)\s+([^\s\[:]+)(?:\[(\d+)])?:\s*(.*)$`)
	// reSyslog5424Header matches the fixed header fields of RFC 5424.
	// Groups: priority, version, timestamp, hostname, app, procid, msgid, rest
	reSyslog5424Header = regexp.MustCompile(`^<(\d+)>(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.*)$`)
	// reSyslog5424SD strips a structured-data element (or the nil "-") from the start.
	reSyslog5424SD = regexp.MustCompile(`^(?:-|\[.*?](?:\[.*?])*)\s*`)
)

type syslogParser struct {
	rfc5424 bool
	tsCache formatCache
}

// newSyslogParser returns a syslog parser; ref (when the source was last
// written) dates RFC 3164 timestamps, which carry no year.
func newSyslogParser(rfc5424 bool, ref time.Time) *syslogParser {
	return &syslogParser{rfc5424: rfc5424, tsCache: formatCache{ref: ref}}
}

// Parse extracts a LogEntry from a single syslog line.
func (p *syslogParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	if p.rfc5424 {
		return p.parse5424(line, lineNum, source)
	}
	return p.parse3164(line, lineNum, source)
}

func (p *syslogParser) parse3164(line string, lineNum int64, source string) (types.LogEntry, bool) {
	m := reSyslog3164Full.FindStringSubmatch(line)
	if m == nil {
		return p.parseISO(line, lineNum, source)
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    m[8],
		Metadata: map[string]string{
			"hostname": m[5],
			"app":      m[6],
		},
	}
	// Files rsyslog writes carry no priority; their level is inferred from
	// the message later.
	if m[1] != "" {
		priority, _ := strconv.Atoi(m[1])
		entry.Level = syslogSeverityToLevel(priority % 8)
	}
	if ts, ok := p.tsCache.Parse(m[2] + " " + m[3] + " " + m[4]); ok {
		entry.Timestamp = ts
	}
	if m[7] != "" {
		entry.Metadata["pid"] = m[7]
	}

	return entry, true
}

// parseISO parses rsyslog's high-precision file format.
func (p *syslogParser) parseISO(line string, lineNum int64, source string) (types.LogEntry, bool) {
	m := reSyslogISOFull.FindStringSubmatch(line)
	if m == nil {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    m[5],
		Metadata: map[string]string{
			"hostname": m[2],
			"app":      m[3],
		},
	}
	if ts, ok := p.tsCache.Parse(m[1]); ok {
		entry.Timestamp = ts
	}
	if m[4] != "" {
		entry.Metadata["pid"] = m[4]
	}

	return entry, true
}

func (p *syslogParser) parse5424(line string, lineNum int64, source string) (types.LogEntry, bool) {
	m := reSyslog5424Header.FindStringSubmatch(line)
	if m == nil {
		return types.LogEntry{}, false
	}

	priority, _ := strconv.Atoi(m[1])
	// m[8] is "STRUCTURED-DATA MSG" — strip the structured data part.
	rest := reSyslog5424SD.ReplaceAllString(m[8], "")

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    strings.TrimSpace(rest),
		Level:      syslogSeverityToLevel(priority % 8),
		Metadata: map[string]string{
			"hostname": m[4],
			"app":      m[5],
		},
	}

	if ts, ok := p.tsCache.Parse(m[3]); ok {
		entry.Timestamp = ts
	}

	if m[6] != "-" {
		entry.Metadata["pid"] = m[6]
	}

	return entry, true
}

// syslogSeverityToLevel maps syslog severity (0-7) to a Level.
func syslogSeverityToLevel(severity int) types.Level {
	switch severity {
	case 0, 1, 2:
		return types.LevelFatal
	case 3:
		return types.LevelError
	case 4, 5:
		return types.LevelWarn
	case 6:
		return types.LevelInfo
	case 7:
		return types.LevelDebug
	default:
		return types.LevelUnknown
	}
}

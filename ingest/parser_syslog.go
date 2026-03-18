package ingest

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oxforge/unlog/types"
)

var (
	reSyslog3164Full = regexp.MustCompile(`^<(\d+)>(\w{3})\s+(\d{1,2})\s+(\d{2}:\d{2}:\d{2})\s+(\S+)\s+(\S+?)(?:\[(\d+)])?:\s*(.*)$`)
	// reSyslog5424Header matches the fixed header fields of RFC 5424.
	// Groups: priority, version, timestamp, hostname, app, procid, msgid, rest
	reSyslog5424Header = regexp.MustCompile(`^<(\d+)>(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.*)$`)
	// reSyslog5424SD strips a structured-data element (or the nil "-") from the start.
	reSyslog5424SD = regexp.MustCompile(`^(?:-|\[.*?](?:\[.*?])*)\s*`)
)

type syslogParser struct {
	rfc5424 bool
	tsCache formatCache
	refYear int // captured once at creation for RFC 3164 year inference
}

func newSyslogParser(rfc5424 bool) *syslogParser {
	return &syslogParser{rfc5424: rfc5424, refYear: time.Now().Year()}
}

func (p *syslogParser) Name() string {
	if p.rfc5424 {
		return "syslog-rfc5424"
	}
	return "syslog-rfc3164"
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
		return types.LogEntry{}, false
	}

	priority, _ := strconv.Atoi(m[1])
	// RFC 3164 timestamps have no year. Assume the current year, adjusting
	// if the resulting time is in the future (e.g., Dec logs parsed in Jan).
	tsStr := m[2] + " " + m[3] + " " + m[4]

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    m[8],
		Level:      syslogSeverityToLevel(priority % 8),
		Metadata: map[string]string{
			"hostname": m[5],
			"app":      m[6],
		},
	}

	if ts, ok := p.tsCache.Parse(tsStr); ok {
		// Infer year: syslog 3164 timestamps lack a year component.
		// Only add the reference year if the parsed time has year 0 (no year
		// in the format string). This guards against timestamp cache formats
		// that include a year, which would cause double-counting.
		// The reference year is captured once at parser creation to avoid
		// per-line time.Now() calls and ensure deterministic behavior.
		if ts.Year() == 0 {
			ts = ts.AddDate(p.refYear, 0, 0)
			if ts.After(time.Date(p.refYear+1, 1, 1, 0, 0, 0, 0, time.UTC)) {
				ts = ts.AddDate(-1, 0, 0)
			}
		}
		entry.Timestamp = ts
	}

	if m[7] != "" {
		entry.Metadata["pid"] = m[7]
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

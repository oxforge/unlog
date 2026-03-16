package ingest

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/oxforge/unlog/types"
)

// reCLFFull matches Apache/Nginx Common Log Format lines.
var reCLFFull = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(\S+)\s+\[([^]]+)]\s+"([^"]*?)"\s+(\d{3})\s+(\d+|-)`)

type clfParser struct {
	tsCache formatCache
}

func (p *clfParser) Name() string { return "clf" }

// Parse extracts a LogEntry from a single CLF log line.
func (p *clfParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	m := reCLFFull.FindStringSubmatch(line)
	if m == nil {
		return types.LogEntry{}, false
	}

	host := m[1]
	tsStr := m[4]
	request := m[5]
	statusStr := m[6]
	sizeStr := m[7]

	status, _ := strconv.Atoi(statusStr)

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    fmt.Sprintf("%s %s", request, statusStr),
		Level:      httpStatusToLevel(status),
		Metadata: map[string]string{
			"host":    host,
			"request": request,
			"status":  statusStr,
			"size":    sizeStr,
		},
	}

	if ts, ok := p.tsCache.Parse(tsStr); ok {
		entry.Timestamp = ts
	}

	return entry, true
}

// httpStatusToLevel maps HTTP status codes to log levels.
func httpStatusToLevel(status int) types.Level {
	switch {
	case status >= 500:
		return types.LevelError
	case status >= 400:
		return types.LevelWarn
	default:
		return types.LevelInfo
	}
}

package ingest

import (
	"strings"

	"github.com/oxforge/unlog/types"
)

// logfmtParser parses key=value formatted log lines.
type logfmtParser struct {
	tsCache formatCache
}

func (p *logfmtParser) Name() string { return "logfmt" }

// Parse extracts well-known fields from a logfmt line into a LogEntry.
// Unknown key=value pairs are stored in Metadata.
func (p *logfmtParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	pairs := parseLogfmtPairs(line)
	if len(pairs) == 0 {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata:   make(map[string]string),
	}

	for k, v := range pairs {
		switch k {
		case "msg", "message":
			entry.Message = v
		case "level", "lvl", "severity":
			entry.Level = types.ParseLevel(v)
		case "ts", "time", "timestamp", "t":
			if ts, ok := p.tsCache.Parse(v); ok {
				entry.Timestamp = ts
			}
		default:
			entry.Metadata[k] = v
		}
	}

	return entry, true
}

// parseLogfmtPairs parses a logfmt-encoded string into a map of key→value pairs.
// Values may be bare tokens or double-quoted strings.
func parseLogfmtPairs(line string) map[string]string {
	pairs := make(map[string]string)
	remaining := line

	for len(remaining) > 0 {
		remaining = strings.TrimLeft(remaining, " \t")
		if len(remaining) == 0 {
			break
		}

		eqIdx := strings.IndexByte(remaining, '=')
		if eqIdx <= 0 {
			break
		}
		key := remaining[:eqIdx]
		remaining = remaining[eqIdx+1:]

		var value string
		if len(remaining) > 0 && remaining[0] == '"' {
			var b strings.Builder
			i := 1
			escaped := false
			for i < len(remaining) {
				ch := remaining[i]
				if escaped {
					b.WriteByte(ch)
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					break
				} else {
					b.WriteByte(ch)
				}
				i++
			}
			value = b.String()
			if i < len(remaining) {
				remaining = remaining[i+1:]
			} else {
				remaining = ""
			}
		} else {
			spaceIdx := strings.IndexAny(remaining, " \t")
			if spaceIdx < 0 {
				value = remaining
				remaining = ""
			} else {
				value = remaining[:spaceIdx]
				remaining = remaining[spaceIdx:]
			}
		}

		pairs[key] = value
	}

	return pairs
}

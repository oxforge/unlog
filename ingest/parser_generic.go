package ingest

import (
	"strings"

	"github.com/oxforge/unlog/types"
)

// genericParser handles plain-text lines that begin with a recognizable timestamp.
// It is the fallback for logs that are not structured but do carry timestamps.
type genericParser struct {
	tsCache formatCache
}

func (p *genericParser) Name() string { return "generic" }

// Parse attempts to extract a timestamp from the beginning of the line and
// the remainder becomes the message. Level is inferred from the first word of
// the message when it matches a known level keyword.
func (p *genericParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
	}

	// Try progressively shorter timestamp prefixes. Scan from the longest
	// plausible length down to the shortest so that more-precise formats
	// are preferred. Skip candidates that don't start with a digit (all
	// supported timestamp formats begin with a year or epoch number),
	// reducing attempts from ~140 to ~20 on cache miss.
	var msgStart int
	maxLen := len(line)
	if maxLen > 35 {
		maxLen = 35
	}
	if maxLen >= 15 && line[0] >= '0' && line[0] <= '9' {
		for length := maxLen; length >= 15; length-- {
			candidate := strings.TrimSpace(line[:length])
			if len(candidate) == 0 || candidate[0] < '0' || candidate[0] > '9' {
				continue
			}
			if ts, ok := p.tsCache.Parse(candidate); ok {
				entry.Timestamp = ts
				msgStart = length
				break
			}
		}
	}

	if entry.Timestamp.IsZero() {
		return types.LogEntry{}, false
	}

	msg := strings.TrimSpace(line[msgStart:])
	entry.Message = msg

	// Peek at the first word: if it is a recognised level token, record it.
	// The word is intentionally left in Message so the caller sees the full
	// original content.
	if spaceIdx := strings.IndexByte(msg, ' '); spaceIdx > 0 {
		candidate := msg[:spaceIdx]
		if level := types.ParseLevel(candidate); level != types.LevelUnknown {
			entry.Level = level
		}
	}

	return entry, true
}

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

	ts, msgStart, ok := p.tsCache.ParseFromPrefix(line)
	if !ok {
		return types.LogEntry{}, false
	}
	entry.Timestamp = ts

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

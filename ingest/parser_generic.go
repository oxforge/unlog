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

// Parse attempts to extract a timestamp from the beginning of the line and
// the remainder becomes the message. Level is inferred from the first word of
// the message when it matches a known level keyword. Trailing key=value pairs
// ("payment failed user=42 amount=10") are lifted into Metadata; when the
// whole remainder is logfmt, its level and msg keys fill Level and Message.
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

	text, pairs := splitTrailingPairs(msg)
	if len(pairs) == 0 {
		return entry, true
	}
	for _, key := range []string{"level", "lvl", "severity"} {
		if v, ok := pairs[key]; ok {
			if entry.Level == types.LevelUnknown {
				entry.Level = types.ParseLevel(v)
			}
			delete(pairs, key)
		}
	}
	for _, key := range []string{"msg", "message"} {
		if v, ok := pairs[key]; ok {
			if text == "" {
				entry.Message = v
			}
			delete(pairs, key)
		}
	}
	if len(pairs) > 0 {
		entry.Metadata = pairs
	}

	return entry, true
}

// splitTrailingPairs splits msg at its first key=value token into the free
// text before it and the parsed pairs from there on.
func splitTrailingPairs(msg string) (string, map[string]string) {
	rest := msg
	offset := 0
	for rest != "" {
		if reLogfmtKey.MatchString(rest) {
			return strings.TrimSpace(msg[:offset]), parseLogfmtPairs(rest)
		}
		i := strings.IndexByte(rest, ' ')
		if i < 0 {
			break
		}
		offset += i + 1
		rest = rest[i+1:]
	}
	return msg, nil
}

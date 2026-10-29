package ingest

import (
	"strings"

	"github.com/oxforge/unlog/types"
)

// dockerComposeParser handles output from `docker compose logs`, where every
// line is prefixed with the service name (with replica number) and a pipe:
//
//	web-1   | 2024-01-15 10:00:00 INFO Starting application
//	redis-1 | 2024-01-15 10:00:01 Ready to accept connections
//
// Lines without a recognizable timestamp after the prefix are still parsed
// (with a zero timestamp), but the line checker requires a timestamp to
// identify new entry boundaries. This means timestamp-less lines from compose
// output are treated as continuations of the preceding entry — which is
// typically correct for stack traces but may fold genuinely independent
// messages into the previous entry when a service doesn't emit timestamps.
type dockerComposeParser struct {
	tsCache formatCache
}

func (p *dockerComposeParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	idx := strings.Index(line, " | ")
	if idx < 0 {
		return types.LogEntry{}, false
	}

	container := strings.TrimSpace(line[:idx])
	rest := line[idx+3:] // skip " | "

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata: map[string]string{
			"container": container,
		},
	}

	msg := rest
	if ts, msgStart, ok := p.tsCache.ParseFromPrefix(rest); ok {
		entry.Timestamp = ts
		msg = strings.TrimSpace(rest[msgStart:])
	}
	applyInnerLine(&entry, msg, &p.tsCache)

	return entry, true
}

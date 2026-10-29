package ingest

import (
	"strings"

	"github.com/oxforge/unlog/types"
)

type kubeParser struct {
	tsCache formatCache
}

func (p *kubeParser) Name() string { return "kubernetes" }

// Parse extracts a LogEntry from a single Kubernetes log line.
// Supports two variants:
//   - CRI format: <timestamp> <stream> <flag> <message>
//   - Prefixed format: [resource/name/container] <timestamp> <level> <message>
//     (from kubectl logs --prefix or similar tooling)
func (p *kubeParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	if len(line) > 0 && line[0] == '[' {
		return p.parsePrefixed(line, lineNum, source)
	}
	return p.parseCRI(line, lineNum, source)
}

// parseCRI handles standard CRI log format: <timestamp> <stream> <flag> <message>
func (p *kubeParser) parseCRI(line string, lineNum int64, source string) (types.LogEntry, bool) {
	// Need at least 4 space-separated parts: timestamp, stream, flag, message.
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return types.LogEntry{}, false
	}

	tsStr := parts[0]
	stream := parts[1]
	msg := parts[3]

	if stream != "stdout" && stream != "stderr" {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata: map[string]string{
			"stream": stream,
		},
	}

	if ts, ok := p.tsCache.Parse(tsStr); ok {
		entry.Timestamp = ts
	}

	applyInnerLine(&entry, msg, &p.tsCache)
	if entry.Level == types.LevelUnknown {
		entry.Level = streamLevel(stream)
	}

	return entry, true
}

// parsePrefixed handles [resource/name/container] <timestamp> <level> <message>
func (p *kubeParser) parsePrefixed(line string, lineNum int64, source string) (types.LogEntry, bool) {
	closeBracket := strings.IndexByte(line, ']')
	if closeBracket < 0 {
		return types.LogEntry{}, false
	}

	resource := line[1:closeBracket]
	rest := strings.TrimSpace(line[closeBracket+1:])

	ts, msgStart, ok := p.tsCache.ParseFromPrefix(rest)
	if !ok {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Timestamp:  ts,
		Metadata: map[string]string{
			"resource": resource,
		},
	}
	applyInnerLine(&entry, strings.TrimSpace(rest[msgStart:]), &p.tsCache)

	return entry, true
}

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
// Kubernetes log format: <timestamp> <stream> <flag> <message>
// where flag is F (full line) or P (partial line).
func (p *kubeParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
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
		Message:    msg,
		Metadata: map[string]string{
			"stream": stream,
		},
	}

	if ts, ok := p.tsCache.Parse(tsStr); ok {
		entry.Timestamp = ts
	}

	if stream == "stderr" {
		entry.Level = types.LevelError
	} else {
		entry.Level = types.LevelInfo
	}

	if spaceIdx := strings.IndexByte(msg, ' '); spaceIdx > 0 {
		candidate := strings.TrimRight(msg[:spaceIdx], ":")
		if level := types.ParseLevel(candidate); level != types.LevelUnknown {
			entry.Level = level
		}
	}

	return entry, true
}

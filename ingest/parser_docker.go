package ingest

import (
	"encoding/json"
	"strings"

	"github.com/oxforge/unlog/types"
)

type dockerParser struct {
	tsCache formatCache
}

func (p *dockerParser) Name() string { return "docker-json" }

// Parse extracts a LogEntry from a single Docker JSON log line.
// Docker JSON format: {"log":"...","stream":"stdout|stderr","time":"..."}
func (p *dockerParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	var obj struct {
		Log    string `json:"log"`
		Stream string `json:"stream"`
		Time   string `json:"time"`
	}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return types.LogEntry{}, false
	}
	if obj.Log == "" && obj.Stream == "" && obj.Time == "" {
		return types.LogEntry{}, false
	}
	if obj.Stream != "stdout" && obj.Stream != "stderr" {
		return types.LogEntry{}, false
	}

	msg := strings.TrimRight(obj.Log, "\n")

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    msg,
		Metadata: map[string]string{
			"stream": obj.Stream,
		},
	}

	if ts, ok := p.tsCache.Parse(obj.Time); ok {
		entry.Timestamp = ts
	}

	if obj.Stream == "stderr" {
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

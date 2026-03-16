package ingest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oxforge/unlog/types"
)

type cloudwatchParser struct {
	tsCache formatCache
}

func (p *cloudwatchParser) Name() string { return "cloudwatch" }

// Parse extracts a LogEntry from a single AWS CloudWatch JSON log line.
// CloudWatch JSON format uses @timestamp and @message fields.
func (p *cloudwatchParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return types.LogEntry{}, false
	}

	atTS, hasTS := obj["@timestamp"]
	atMsg, hasMsg := obj["@message"]
	if !hasTS || !hasMsg {
		return types.LogEntry{}, false
	}

	msg := strings.TrimSpace(fmt.Sprint(atMsg))

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    msg,
		Metadata:   make(map[string]string),
	}

	if ts, ok := p.tsCache.Parse(fmt.Sprint(atTS)); ok {
		entry.Timestamp = ts
	}

	if logStream, ok := obj["@logStream"]; ok {
		entry.Metadata["logStream"] = fmt.Sprint(logStream)
	}

	if spaceIdx := strings.IndexByte(msg, ' '); spaceIdx > 0 {
		candidate := strings.TrimRight(msg[:spaceIdx], ":")
		if level := types.ParseLevel(candidate); level != types.LevelUnknown {
			entry.Level = level
		}
	}

	return entry, true
}

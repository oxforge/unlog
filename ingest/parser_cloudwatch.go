package ingest

import (
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
	obj, ok := decodeJSONObject(line)
	if !ok {
		return types.LogEntry{}, false
	}

	atTS, hasTS := obj["@timestamp"]
	atMsg, hasMsg := obj["@message"]
	if !hasTS || !hasMsg {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata:   make(map[string]string),
	}

	if ts, ok := p.tsCache.Parse(fmt.Sprint(atTS)); ok {
		entry.Timestamp = ts
	}

	if logStream, ok := obj["@logStream"]; ok {
		entry.Metadata["logStream"] = fmt.Sprint(logStream)
	}

	applyInnerLine(&entry, strings.TrimSpace(fmt.Sprint(atMsg)), &p.tsCache)

	return entry, true
}

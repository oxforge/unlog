package ingest

import (
	"encoding/json"
	"fmt"

	"github.com/oxforge/unlog/types"
)

// jsonParser parses structured JSON log lines.
type jsonParser struct {
	tsCache formatCache
}

func (p *jsonParser) Name() string { return "json" }

// Parse decodes a JSON object and extracts well-known fields into a LogEntry.
// Unknown fields are preserved in Metadata.
func (p *jsonParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata:   make(map[string]string),
	}

	for _, key := range []string{"msg", "message", "log"} {
		if v, ok := obj[key]; ok {
			entry.Message = fmt.Sprint(v)
			delete(obj, key)
			break
		}
	}

	for _, key := range []string{"level", "severity", "lvl"} {
		if v, ok := obj[key]; ok {
			entry.Level = types.ParseLevel(fmt.Sprint(v))
			delete(obj, key)
			break
		}
	}

	for _, key := range []string{"timestamp", "ts", "time", "@timestamp", "t"} {
		if v, ok := obj[key]; ok {
			if ts, ok := p.tsCache.Parse(fmt.Sprint(v)); ok {
				entry.Timestamp = ts
			}
			delete(obj, key)
			break
		}
	}

	for k, v := range obj {
		entry.Metadata[k] = fmt.Sprint(v)
	}

	return entry, true
}

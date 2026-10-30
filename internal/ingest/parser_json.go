package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/oxforge/unlog/internal/types"
)

// jsonParser parses structured JSON log lines.
type jsonParser struct {
	tsCache formatCache
}

// Parse decodes a JSON object and extracts well-known fields into a LogEntry.
// Unknown fields are preserved in Metadata.
func (p *jsonParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	obj, ok := decodeJSONObject(line)
	if !ok {
		return types.LogEntry{}, false
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata:   make(map[string]string),
	}
	applyJSONFields(&entry, obj, &p.tsCache)

	return entry, true
}

// applyJSONFields sets Message, Level and Timestamp from obj's well-known
// keys, when present, and copies the other keys into entry.Metadata without
// overwriting a key already there. entry.Metadata must not be nil.
func applyJSONFields(entry *types.LogEntry, obj map[string]interface{}, tsCache *formatCache) {
	for _, key := range []string{"msg", "message", "log"} {
		if v, ok := obj[key]; ok {
			entry.Message = fmt.Sprint(v)
			delete(obj, key)
			break
		}
	}

	for _, key := range []string{"level", "severity", "lvl"} {
		if v, ok := obj[key]; ok {
			entry.Level = parseJSONLevel(v)
			delete(obj, key)
			break
		}
	}

	for _, key := range []string{"timestamp", "ts", "time", "@timestamp", "t"} {
		if v, ok := obj[key]; ok {
			if ts, zoned, ok := tsCache.ParseZoned(fmt.Sprint(v)); ok && (zoned || entry.Timestamp.IsZero()) {
				entry.Timestamp = ts
			}
			delete(obj, key)
			break
		}
	}

	for k, v := range obj {
		if _, taken := entry.Metadata[k]; taken || v == nil {
			continue
		}
		switch v.(type) {
		case map[string]interface{}, []interface{}:
			// Nested values stay JSON, not Go's map[...] notation.
			if b, err := json.Marshal(v); err == nil {
				entry.Metadata[k] = string(b)
			}
		default:
			entry.Metadata[k] = fmt.Sprint(v)
		}
	}
}

// decodeJSONObject decodes line as a single JSON object. Numbers are kept as
// json.Number, so epoch timestamps and large IDs keep every digit instead of
// passing through float64. Anything after the object other than whitespace
// rejects the line.
func decodeJSONObject(line string) (map[string]interface{}, bool) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var obj map[string]interface{}
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return obj, true
}

// parseJSONLevel maps a level field. Numbers of 10 and above are pino/bunyan
// levels (10 trace … 60 fatal); smaller numbers are syslog severities (0–7).
func parseJSONLevel(v interface{}) types.Level {
	num, ok := v.(json.Number)
	if !ok {
		return types.ParseLevel(fmt.Sprint(v))
	}
	n, err := strconv.ParseInt(num.String(), 10, 64)
	if err != nil {
		return types.LevelUnknown
	}
	switch {
	case n >= 60:
		return types.LevelFatal
	case n >= 50:
		return types.LevelError
	case n >= 40:
		return types.LevelWarn
	case n >= 30:
		return types.LevelInfo
	case n >= 20:
		return types.LevelDebug
	case n >= 10:
		return types.LevelTrace
	default:
		return types.ParseLevel(num.String())
	}
}

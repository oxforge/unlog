package ingest

import (
	"encoding/csv"
	"strings"

	"github.com/oxforge/unlog/types"
)

// csvParser parses CSV-formatted log lines. A header row is required as the
// first line to map columns to fields. Files without a recognised header are
// rejected (all lines return false).
type csvParser struct {
	tsCache    formatCache
	columns    []string
	configured bool
	noHeader   bool
}

func (p *csvParser) Name() string { return "csv" }

// knownHeaders are column names that signal a CSV header row.
var knownHeaders = map[string]bool{
	"timestamp": true, "time": true, "ts": true, "date": true, "datetime": true,
	"level": true, "severity": true, "lvl": true, "loglevel": true,
	"message": true, "msg": true, "log": true,
	"source": true, "service": true, "host": true, "hostname": true,
}

// isHeaderRow returns true if most fields look like known column names.
func isHeaderRow(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	matches := 0
	for _, f := range fields {
		if knownHeaders[strings.ToLower(strings.TrimSpace(f))] {
			matches++
		}
	}
	return matches >= 2
}

func (p *csvParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	if p.noHeader {
		return types.LogEntry{}, false
	}

	fields, ok := parseCSVLine(line)
	if !ok || len(fields) < 2 {
		return types.LogEntry{}, false
	}

	// First call: require a header row.
	if !p.configured {
		p.configured = true
		if !isHeaderRow(fields) {
			p.noHeader = true
			return types.LogEntry{}, false
		}
		p.columns = make([]string, len(fields))
		for i, f := range fields {
			p.columns[i] = strings.ToLower(strings.TrimSpace(f))
		}
		return types.LogEntry{}, false // skip header
	}

	entry := types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Metadata:   make(map[string]string),
	}

	for i, col := range p.columns {
		if i >= len(fields) {
			break
		}
		val := strings.TrimSpace(fields[i])
		switch col {
		case "timestamp", "time", "ts", "date", "datetime":
			if ts, ok := p.tsCache.Parse(val); ok {
				entry.Timestamp = ts
			}
		case "level", "severity", "lvl", "loglevel":
			entry.Level = types.ParseLevel(val)
		case "message", "msg", "log":
			entry.Message = val
		default:
			if val != "" {
				entry.Metadata[col] = val
			}
		}
	}

	return entry, true
}

// parseCSVLine parses a single CSV line using encoding/csv.
func parseCSVLine(line string) ([]string, bool) {
	r := csv.NewReader(strings.NewReader(line))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	fields, err := r.Read()
	if err != nil {
		return nil, false
	}
	return fields, true
}

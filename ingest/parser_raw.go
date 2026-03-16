package ingest

import "github.com/oxforge/unlog/types"

// rawParser is the unconditional fallback: every line becomes an entry with no
// timestamp and unknown level. It never rejects a line.
type rawParser struct{}

func (p *rawParser) Name() string { return "raw" }

// Parse always succeeds. The full line is used as the message verbatim.
func (p *rawParser) Parse(line string, lineNum int64, source string) (types.LogEntry, bool) {
	return types.LogEntry{
		RawLine:    line,
		LineNumber: lineNum,
		Source:     source,
		Message:    line,
		Level:      types.LevelUnknown,
	}, true
}

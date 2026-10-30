package filter

import "regexp"

var (
	reUUID   = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reTS     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	reIPv4   = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	reIPv6   = regexp.MustCompile(`[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){7}`)
	reHex    = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	rePath   = regexp.MustCompile(`/[\w./\-]+`)
	reQuoted = regexp.MustCompile(`"[^"]*"`)
)

// reCombined merges all patterns into a single regex. Most-specific patterns
// come first (e.g. UUID before hex, timestamp before number).
var reCombined = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` + `|` +
		`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}` + `|` +
		`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}` + `|` +
		`[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){7}` + `|` +
		`\b[0-9a-fA-F]{8,}\b` + `|` +
		`/[\w./\-]+` + `|` +
		`"[^"]*"` + `|` +
		`\b\d+`,
)

// ExtractSignature replaces variable parts of a log message with placeholders,
// grouping messages that differ only in IDs, timestamps, IPs, etc.
func ExtractSignature(message string) string {
	return reCombined.ReplaceAllStringFunc(message, classifyMatch)
}

// classifyMatch determines the placeholder for a regex match.
func classifyMatch(match string) string {
	switch {
	case reUUID.MatchString(match):
		return "<UUID>"
	case reTS.MatchString(match):
		return "<TS>"
	case reIPv4.MatchString(match):
		return "<IP>"
	case reIPv6.MatchString(match):
		return "<IP>"
	case reHex.MatchString(match):
		return "<HEX>"
	case rePath.MatchString(match):
		return "<PATH>"
	case reQuoted.MatchString(match):
		return "<STR>"
	default:
		return "<NUM>"
	}
}

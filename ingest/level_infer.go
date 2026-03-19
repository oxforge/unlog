package ingest

import (
	"strings"

	"github.com/oxforge/unlog/types"
)

// InferLevel scans the first 5 space-separated tokens of msg for a level
// keyword. Surrounding punctuation (brackets, colons, dashes, parens) is
// stripped before matching. Returns the first match or LevelUnknown.
func InferLevel(msg string) types.Level {
	const maxTokens = 5
	rest := msg
	for i := 0; i < maxTokens && rest != ""; i++ {
		token, tail := nextToken(rest)
		rest = tail
		cleaned := stripPunctuation(token)
		if cleaned == "" || isNumeric(cleaned) {
			continue
		}
		if lvl := types.ParseLevel(cleaned); lvl != types.LevelUnknown {
			return lvl
		}
	}
	return types.LevelUnknown
}

func nextToken(s string) (token, rest string) {
	s = strings.TrimLeft(s, " ")
	i := strings.IndexByte(s, ' ')
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// stripPunctuation removes surrounding brackets, colons, parens, equals, and
// dashes from s. The = covers logfmt-style tokens like "=ERROR=", and - covers
// dash-delimited separators like "---ERROR---".
func stripPunctuation(s string) string {
	return strings.Trim(s, "[]():=-")
}

// isNumeric returns true if s consists only of digits (and optional dots for
// decimals). ParseLevel maps syslog numeric severities like "2" → Fatal, but
// those shouldn't match free-text tokens like "5" in "in 5 seconds".
func isNumeric(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' {
			return false
		}
	}
	return true
}

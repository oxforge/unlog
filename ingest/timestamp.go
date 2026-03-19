package ingest

import (
	"strconv"
	"strings"
	"time"
)

// timestampFormats is an ordered list of formats to try when parsing timestamps.
// Ordered by prevalence — most common formats first to minimize attempts.
var timestampFormats = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999",
	"2006-01-02 15:04:05,999", // Java-style comma millis (e.g. log4j)
	"2006-01-02 15:04:05",
	"02/Jan/2006:15:04:05 -0700",
	"Jan  2 15:04:05",
	"Jan 2 15:04:05",
	"Jan 2, 2006 @ 15:04:05.000", // Kibana CSV export, e.g. "Feb 25, 2026 @ 03:32:24.771"
}

// parseTimestamp attempts to parse s as a timestamp, trying each known format
// in order. Returns the parsed time, the matched format string, and whether
// parsing succeeded. The returned time is always in UTC.
func parseTimestamp(s string) (time.Time, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, "", false
	}

	for _, fmt := range timestampFormats {
		if t, err := time.Parse(fmt, s); err == nil {
			return t.UTC(), fmt, true
		}
	}

	if t, ok := tryUnixEpoch(s); ok {
		return t.UTC(), "epoch", true
	}

	return time.Time{}, "", false
}

// tryUnixEpoch attempts to interpret s as a Unix epoch value (seconds or millis).
func tryUnixEpoch(s string) (time.Time, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	switch {
	case n > 1e15:
		// Too large to be a plausible epoch value.
		return time.Time{}, false
	case n > 1e12:
		return time.UnixMilli(n), true
	case n > 1e9:
		return time.Unix(n, 0), true
	default:
		return time.Time{}, false
	}
}

// formatCache caches the last successfully matched timestamp format for a
// single log source, so that subsequent lines avoid re-trying all formats.
type formatCache struct {
	lastFormat string
}

// ParseFromPrefix scans the beginning of s for a timestamp, trying
// progressively shorter prefixes (max 35 chars down to 15). Returns the
// parsed time, the byte offset where the remainder starts, and success.
func (c *formatCache) ParseFromPrefix(s string) (time.Time, int, bool) {
	maxLen := len(s)
	if maxLen > 35 {
		maxLen = 35
	}
	if maxLen < 15 || s[0] < '0' || s[0] > '9' {
		return time.Time{}, 0, false
	}
	for length := maxLen; length >= 15; length-- {
		candidate := strings.TrimSpace(s[:length])
		if len(candidate) == 0 || candidate[0] < '0' || candidate[0] > '9' {
			continue
		}
		if t, ok := c.Parse(candidate); ok {
			return t, length, true
		}
	}
	return time.Time{}, 0, false
}

// Parse attempts to parse s using the cached format first, then falls back to
// the full format list and updates the cache on success.
func (c *formatCache) Parse(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}

	if c.lastFormat != "" {
		if c.lastFormat == "epoch" {
			if t, ok := tryUnixEpoch(s); ok {
				return t.UTC(), true
			}
		} else {
			if t, err := time.Parse(c.lastFormat, s); err == nil {
				return t.UTC(), true
			}
		}
	}

	t, fmt, ok := parseTimestamp(s)
	if ok {
		c.lastFormat = fmt
	}
	return t, ok
}

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

// parse attempts to parse s as a timestamp, trying each known format in
// order. Returns the parsed time, the matched format string, and whether
// parsing succeeded. The returned time is always in UTC.
func (c *formatCache) parse(s string) (time.Time, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, "", false
	}

	for _, layout := range timestampFormats {
		if t, err := c.parseLayout(layout, s); err == nil {
			return t, layout, true
		}
	}

	if t, ok := tryUnixEpoch(s); ok {
		return t.UTC(), "epoch", true
	}

	return time.Time{}, "", false
}

// parseLayout parses s with layout. A value without a zone is read in the
// cache's zone. A value without a year is read as a wall-clock time and dated
// from ref first: Go would place it in year 0, where zones have only their
// 19th-century local mean time and no daylight saving.
func (c *formatCache) parseLayout(layout, s string) (time.Time, error) {
	if strings.Contains(layout, "2006") {
		t, err := time.ParseInLocation(layout, s, c.zone())
		return t.UTC(), err
	}
	wall, err := time.Parse(layout, s)
	if err != nil {
		return time.Time{}, err
	}
	return c.withYear(wall), nil
}

// withYear places a year-less wall-clock time in ref's year in the cache's
// zone, or the year before when that would be more than a day after ref.
func (c *formatCache) withYear(wall time.Time) time.Time {
	ref := c.ref
	if ref.IsZero() {
		ref = time.Now()
	}
	in := func(year int) time.Time {
		return time.Date(year, wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), c.zone()).UTC()
	}
	year := ref.In(c.zone()).Year()
	if t := in(year); !t.After(ref.Add(24 * time.Hour)) {
		return t
	}
	return in(year - 1)
}

// hasZone reports whether a value parsed with layout names its own zone.
func hasZone(layout string) bool {
	return layout == "epoch" || strings.Contains(layout, "Z07") || strings.Contains(layout, "-0700")
}

// tryUnixEpoch attempts to interpret s as a Unix epoch value in seconds,
// milliseconds, microseconds or nanoseconds, chosen by magnitude. Seconds may
// carry a fractional part ("1705312801.123456", as zap writes them); it is
// parsed from the digits, not through a float, so it stays exact.
func tryUnixEpoch(s string) (time.Time, bool) {
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	n, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil || n <= 1e9 {
		return time.Time{}, false
	}
	var nanos int64
	if hasFrac {
		if fracPart == "" || len(fracPart) > 9 || n > 1e11 {
			return time.Time{}, false
		}
		frac, err := strconv.ParseUint(fracPart, 10, 64)
		if err != nil {
			return time.Time{}, false
		}
		for i := len(fracPart); i < 9; i++ {
			frac *= 10
		}
		nanos = int64(frac)
	}
	switch {
	case n > 1e17:
		return time.Unix(0, n), true
	case n > 1e14:
		return time.UnixMicro(n), true
	case n > 1e11:
		return time.UnixMilli(n), true
	default:
		return time.Unix(n, nanos), true
	}
}

// formatCache caches the last successfully matched timestamp format for a
// single log source, so that subsequent lines avoid re-trying all formats.
type formatCache struct {
	lastFormat string
	// loc is the zone of timestamps that carry none; nil means UTC.
	loc *time.Location
	// ref is when the source was last written. Timestamps that carry no year
	// (RFC 3164 syslog) get ref's year, or the year before when that would
	// put them more than a day after ref (December lines read in January).
	// Zero means now.
	ref time.Time
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
		} else if t, err := c.parseLayout(c.lastFormat, s); err == nil {
			return t, true
		}
	}

	t, layout, ok := c.parse(s)
	if ok {
		c.lastFormat = layout
	}
	return t, ok
}

// ParseZoned is Parse that also reports whether the value named its own zone.
func (c *formatCache) ParseZoned(s string) (time.Time, bool, bool) {
	t, ok := c.Parse(s)
	return t, ok && hasZone(c.lastFormat), ok
}

// zone is the location for timestamps without a zone: loc, or UTC.
func (c *formatCache) zone() *time.Location {
	if c.loc == nil {
		return time.UTC
	}
	return c.loc
}

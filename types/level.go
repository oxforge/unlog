package types

import "strings"

type Level int

const (
	LevelUnknown Level = iota
	LevelTrace
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func ParseLevel(s string) Level {
	upper := strings.ToUpper(strings.TrimSpace(s))
	switch upper {
	case "TRACE", "T":
		return LevelTrace
	case "DEBUG", "D":
		return LevelDebug
	case "INFO", "I":
		return LevelInfo
	case "WARN", "WARNING", "W":
		return LevelWarn
	case "ERROR", "ERR", "E":
		return LevelError
	case "FATAL", "CRITICAL", "CRIT", "F":
		return LevelFatal
	case "0", "1", "2":
		return LevelFatal
	case "3":
		return LevelError
	case "4", "5":
		return LevelWarn
	case "6":
		return LevelInfo
	case "7":
		return LevelDebug
	default:
		return LevelUnknown
	}
}

func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Meets reports whether l is at or above the given threshold.
// LevelUnknown never meets any threshold — the filter stage handles
// unknown-level entries separately (they are always kept) to avoid
// silently dropping entries whose severity cannot be determined.
func (l Level) Meets(threshold Level) bool {
	return l >= threshold && l != LevelUnknown
}

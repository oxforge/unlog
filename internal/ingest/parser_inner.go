package ingest

import (
	"strings"

	"github.com/oxforge/unlog/internal/types"
)

// applyInnerLine reads msg, the line a container runtime or log service
// wrapped (Docker's "log", CRI's message, CloudWatch's "@message"), as a log
// line of its own. A JSON object or logfmt pairs fill Message, Level,
// Timestamp and Metadata; the wrapper's timestamp stays unless the payload's
// names its own zone, and the wrapper's metadata keys are never overwritten.
// Plain text stays the message, with its level read from its first words.
func applyInnerLine(entry *types.LogEntry, msg string, tsCache *formatCache) {
	entry.Message = msg
	if strings.HasPrefix(msg, "{") {
		if obj, ok := decodeJSONObject(msg); ok {
			applyJSONFields(entry, obj, tsCache)
			return
		}
	}
	if isLogfmtLine(msg) {
		applyLogfmtFields(entry, parseLogfmtPairs(msg), tsCache)
		return
	}
	entry.Level = plainTextLevel(msg)
}

// plainTextLevel returns the level named by msg's first word ("ERROR:
// ..."), or else by one of its first few words ("[2024-01-15 10:00:05] WARN:
// ...").
func plainTextLevel(msg string) types.Level {
	if i := strings.IndexByte(msg, ' '); i > 0 {
		if level := types.ParseLevel(strings.TrimRight(msg[:i], ":")); level != types.LevelUnknown {
			return level
		}
	}
	return InferLevel(msg)
}

// streamLevel is the fallback level for a container line whose content names
// none. stdout carries normal output; stderr says nothing about severity,
// since most runtimes and logging libraries write every level there.
func streamLevel(stream string) types.Level {
	if stream == "stdout" {
		return types.LevelInfo
	}
	return types.LevelUnknown
}

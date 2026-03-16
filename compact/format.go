package compact

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oxforge/unlog/types"
)

// Budget fractions for each output section. They sum to 1.0.
const (
	budgetOverview  = 0.05
	budgetCritical  = 0.45
	budgetChains    = 0.20
	budgetAnomalies = 0.10
	budgetContext   = 0.20
)

const (
	sectionOverview  = "Incident Overview"
	sectionCritical  = "Critical Errors"
	sectionChains    = "Error Chains"
	sectionAnomalies = "Rate Anomalies"
	sectionContext   = "Context"
)

// estimatedHeaderTokens is the approximate token cost of "## Section Name\n".
// We subtract this from each section's budget so headers don't cause over-runs.
const estimatedHeaderTokens = 5

// formatSummary produces the structured text summary from scored and grouped entries.
// Sections are mutually exclusive; priority: Error Chains > Rate Anomalies > Critical Errors > Context.
func formatSummary(all []scoredEntry, tokenBudget int) string {
	var sb strings.Builder

	claimed := make(map[int]bool)

	var chainsRaw []scoredEntry
	for i, se := range all {
		if se.entry.ChainID != "" && (se.entry.Level == types.LevelFatal || se.entry.Level == types.LevelError) {
			chainsRaw = append(chainsRaw, se)
			claimed[i] = true
		}
	}
	chains := deduplicateByChain(chainsRaw)

	var anomalies []scoredEntry
	for i, se := range all {
		if !claimed[i] && se.entry.IsSpike {
			anomalies = append(anomalies, se)
			claimed[i] = true
		}
	}

	var criticals []scoredEntry
	for i, se := range all {
		if !claimed[i] && (se.entry.Level == types.LevelFatal || se.entry.Level == types.LevelError) {
			criticals = append(criticals, se)
			claimed[i] = true
		}
	}

	var contextEntries []scoredEntry
	for i, se := range all {
		if !claimed[i] && (se.entry.Level == types.LevelWarn || se.entry.Level == types.LevelInfo) {
			contextEntries = append(contextEntries, se)
		}
	}

	overviewBudget := sectionBudget(tokenBudget, budgetOverview)
	overviewText := buildOverview(all)
	writeSection(&sb, sectionOverview, truncateToTokens(overviewText, overviewBudget))
	writeEntriesSection(&sb, sectionCritical, criticals, sectionBudget(tokenBudget, budgetCritical))
	writeEntriesSection(&sb, sectionChains, chains, sectionBudget(tokenBudget, budgetChains))
	writeEntriesSection(&sb, sectionAnomalies, anomalies, sectionBudget(tokenBudget, budgetAnomalies))
	writeEntriesSection(&sb, sectionContext, contextEntries, sectionBudget(tokenBudget, budgetContext))

	return strings.TrimRight(sb.String(), "\n")
}

// sectionBudget computes the token budget for a section, subtracting the
// estimated header overhead so headers don't cause over-runs.
func sectionBudget(totalBudget int, fraction float64) int {
	budget := int(float64(totalBudget)*fraction) - estimatedHeaderTokens
	if budget < 0 {
		return 0
	}
	return budget
}

// buildOverview produces a short incident summary paragraph from all entries.
func buildOverview(all []scoredEntry) string {
	if len(all) == 0 {
		return "No significant log entries found."
	}

	var earliest, latest time.Time
	var errorCount, fatalCount, warnCount int
	sources := make(map[string]struct{})
	var chainIDs []string
	chainSeen := make(map[string]struct{})
	spikeCount := 0

	for _, se := range all {
		e := se.entry
		t := e.Timestamp
		if t.IsZero() {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
		switch e.Level {
		case types.LevelFatal:
			fatalCount++
		case types.LevelError:
			errorCount++
		case types.LevelWarn:
			warnCount++
		}
		if e.Source != "" {
			sources[e.Source] = struct{}{}
		}
		if e.ChainID != "" {
			if _, seen := chainSeen[e.ChainID]; !seen {
				chainSeen[e.ChainID] = struct{}{}
				chainIDs = append(chainIDs, e.ChainID)
			}
		}
		if e.IsSpike {
			spikeCount++
		}
	}

	duration := latest.Sub(earliest).Round(time.Second)

	var parts []string
	parts = append(parts, fmt.Sprintf("Window: %s — %s (%s)",
		earliest.UTC().Format(time.RFC3339),
		latest.UTC().Format(time.RFC3339),
		duration))
	parts = append(parts, fmt.Sprintf("Sources: %d (%s)", len(sources), joinedSources(sources)))
	parts = append(parts, fmt.Sprintf("Events: %d fatal, %d error, %d warn", fatalCount, errorCount, warnCount))

	if len(chainIDs) > 0 {
		parts = append(parts, fmt.Sprintf("Error chains detected: %s", strings.Join(chainIDs, ", ")))
	}
	if spikeCount > 0 {
		parts = append(parts, fmt.Sprintf("Rate spikes: %d", spikeCount))
	}

	return strings.Join(parts, "\n")
}

// joinedSources returns a comma-separated sorted list of source names.
func joinedSources(m map[string]struct{}) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) > 5 {
		return strings.Join(names[:5], ", ") + fmt.Sprintf(", +%d more", len(names)-5)
	}
	return strings.Join(names, ", ")
}

// writeSection writes a section header and body to sb.
func writeSection(sb *strings.Builder, title, body string) {
	sb.WriteString("## ")
	sb.WriteString(title)
	sb.WriteString("\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

// writeEntriesSection renders a capped set of entries into a section.
// The tokenBudget accounts for entry content only (header overhead is pre-subtracted).
func writeEntriesSection(sb *strings.Builder, title string, entries []scoredEntry, tokenBudget int) {
	sb.WriteString("## ")
	sb.WriteString(title)
	sb.WriteString("\n")

	if len(entries) == 0 {
		sb.WriteString("(none)\n\n")
		return
	}

	used := 0
	written := 0
	for _, se := range entries {
		line := formatEntry(se.entry)
		tokens := EstimateTokens(line)
		if used+tokens > tokenBudget && written > 0 {
			remaining := len(entries) - written
			if remaining > 0 {
				truncLine := fmt.Sprintf("... (%d more entries omitted)\n", remaining)
				sb.WriteString(truncLine)
			}
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		used += tokens
		written++
	}

	sb.WriteString("\n")
}

// formatEntry renders a single EnrichedEntry as a log-like line.
func formatEntry(e types.EnrichedEntry) string {
	var sb strings.Builder

	sb.WriteString(e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"))
	sb.WriteString(" [")
	sb.WriteString(e.Level.String())
	sb.WriteString("] ")
	if e.Source != "" {
		sb.WriteString(e.Source)
		sb.WriteString(": ")
	}
	sb.WriteString(e.Message)

	var tags []string
	if e.ChainID != "" {
		tags = append(tags, "chain="+e.ChainID)
	}
	if e.IsDeployment {
		tags = append(tags, "deploy")
	}
	if e.IsSpike {
		tags = append(tags, "spike")
	}
	if e.OccurrenceCount > 1 {
		tags = append(tags, fmt.Sprintf("×%d", e.OccurrenceCount))
	}
	if e.HTTPStatus > 0 {
		tags = append(tags, fmt.Sprintf("http=%d", e.HTTPStatus))
	}
	if e.ErrorType != "" {
		tags = append(tags, "err="+e.ErrorType)
	}
	if e.TraceID != "" {
		tags = append(tags, "trace="+e.TraceID)
	}

	if len(tags) > 0 {
		sb.WriteString(" {")
		sb.WriteString(strings.Join(tags, " "))
		sb.WriteString("}")
	}

	return sb.String()
}

// deduplicateByChain returns at most one entry per chain ID (the highest-scored).
func deduplicateByChain(entries []scoredEntry) []scoredEntry {
	best := make(map[string]scoredEntry)
	for _, se := range entries {
		if se.entry.ChainID == "" {
			continue
		}
		if existing, seen := best[se.entry.ChainID]; !seen || se.score > existing.score {
			best[se.entry.ChainID] = se
		}
	}

	seen := make(map[string]bool)
	var result []scoredEntry
	for _, se := range entries {
		if se.entry.ChainID == "" {
			result = append(result, se)
			continue
		}
		if !seen[se.entry.ChainID] {
			seen[se.entry.ChainID] = true
			result = append(result, best[se.entry.ChainID])
		}
	}
	return result
}

// truncateToTokens truncates s so its estimated token count does not exceed budget.
// It snaps to the last newline within budget to avoid cutting mid-line.
func truncateToTokens(s string, budget int) string {
	if EstimateTokens(s) <= budget {
		return s
	}
	// Approximate target byte length from token budget.
	// Inverse of (len*2+6)/7 → len ≈ budget*7/2.
	targetBytes := budget * 7 / 2
	if targetBytes >= len(s) {
		return s
	}
	// Snap back to last newline within budget to avoid cutting mid-line.
	cut := s[:targetBytes]
	if idx := strings.LastIndexByte(cut, '\n'); idx > 0 {
		cut = cut[:idx]
	}
	return cut + "\n... (truncated)"
}

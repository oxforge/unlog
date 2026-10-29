package compact

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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

// maxMessageBytes caps how much of one entry's message the summary shows, so
// a single huge line (a payload dump, a long stack trace) cannot fill a
// section or the LLM prompt on its own.
const maxMessageBytes = 2000

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
		if !claimed[i] {
			contextEntries = append(contextEntries, se)
		}
	}

	overviewBudget := sectionBudget(tokenBudget, budgetOverview)
	overviewText := buildOverview(all)
	writeSection(&sb, sectionOverview, truncateToTokens(overviewText, overviewBudget))

	// Listed in the order leftover budget is handed out.
	sections := []*entrySection{
		newEntrySection(sectionCritical, criticals, sectionBudget(tokenBudget, budgetCritical)),
		newEntrySection(sectionChains, chains, sectionBudget(tokenBudget, budgetChains)),
		newEntrySection(sectionAnomalies, anomalies, sectionBudget(tokenBudget, budgetAnomalies)),
		newEntrySection(sectionContext, contextEntries, sectionBudget(tokenBudget, budgetContext)),
	}
	shareUnusedBudget(sections)
	for _, sec := range sections {
		sec.write(&sb)
	}

	return strings.TrimRight(sb.String(), "\n")
}

// entrySection is one section of entries with its rendered lines and budget.
type entrySection struct {
	title  string
	items  []scoredEntry // highest score first
	lines  []string      // lines[i] renders items[i]
	tokens []int         // tokens[i] is the cost of lines[i] with its newline
	budget int
}

func newEntrySection(title string, items []scoredEntry, budget int) *entrySection {
	sec := &entrySection{title: title, items: items, budget: budget}
	for _, se := range items {
		line := formatEntry(se.entry)
		sec.lines = append(sec.lines, line)
		sec.tokens = append(sec.tokens, EstimateTokens(line+"\n"))
	}
	return sec
}

// need is the token cost of listing every entry in the section.
func (sec *entrySection) need() int {
	total := 0
	for _, t := range sec.tokens {
		total += t
	}
	return total
}

// shareUnusedBudget caps each section's budget at what it needs and hands the
// rest, in section order, to sections that would otherwise be truncated.
func shareUnusedBudget(sections []*entrySection) {
	leftover := 0
	for _, sec := range sections {
		if need := sec.need(); need < sec.budget {
			leftover += sec.budget - need
			sec.budget = need
		}
	}
	for _, sec := range sections {
		if leftover == 0 {
			return
		}
		extra := min(sec.need()-sec.budget, leftover)
		if extra > 0 {
			sec.budget += extra
			leftover -= extra
		}
	}
}

// write renders the section: the highest-scored entries that fit the budget
// (always at least one), listed in time order with undated entries last.
func (sec *entrySection) write(sb *strings.Builder) {
	sb.WriteString("## ")
	sb.WriteString(sec.title)
	sb.WriteString("\n")

	if len(sec.items) == 0 {
		sb.WriteString("(none)\n\n")
		return
	}

	var chosen []int
	used := 0
	for i := range sec.items {
		if used+sec.tokens[i] > sec.budget && len(chosen) > 0 {
			break
		}
		chosen = append(chosen, i)
		used += sec.tokens[i]
	}

	sort.SliceStable(chosen, func(a, b int) bool {
		ta, tb := sec.items[chosen[a]].entry.Timestamp, sec.items[chosen[b]].entry.Timestamp
		if ta.IsZero() != tb.IsZero() {
			return tb.IsZero()
		}
		return ta.Before(tb)
	})
	for _, i := range chosen {
		sb.WriteString(sec.lines[i])
		sb.WriteString("\n")
	}
	if omitted := len(sec.items) - len(chosen); omitted > 0 {
		fmt.Fprintf(sb, "... (%d more entries omitted)\n", omitted)
	}
	sb.WriteString("\n")
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
	var fatal, errs, warn levelTally
	sources := make(map[string]struct{})
	var chainIDs []string
	chainSeen := make(map[string]struct{})
	spikeCount := 0

	for _, se := range all {
		e := se.entry
		if t := e.Timestamp; !t.IsZero() {
			if earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
			end := t
			if e.LastSeen.After(end) {
				end = e.LastSeen
			}
			if end.After(latest) {
				latest = end
			}
		}
		switch e.Level {
		case types.LevelFatal:
			fatal.add(e)
		case types.LevelError:
			errs.add(e)
		case types.LevelWarn:
			warn.add(e)
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

	var parts []string
	if earliest.IsZero() {
		parts = append(parts, "Window: unknown (no timestamps)")
	} else {
		parts = append(parts, fmt.Sprintf("Window: %s — %s (%s)",
			earliest.UTC().Format(time.RFC3339),
			latest.UTC().Format(time.RFC3339),
			latest.Sub(earliest).Round(time.Second)))
	}
	parts = append(parts, fmt.Sprintf("Sources: %d (%s)", len(sources), joinedSources(sources)))
	parts = append(parts, fmt.Sprintf("Events: %s, %s, %s",
		fatal.describe("fatal"), errs.describe("error"), warn.describe("warn")))

	if len(chainIDs) > 0 {
		parts = append(parts, fmt.Sprintf("Error chains detected: %s", strings.Join(chainIDs, ", ")))
	}
	if spikeCount > 0 {
		parts = append(parts, fmt.Sprintf("Rate spikes: %d", spikeCount))
	}

	return strings.Join(parts, "\n")
}

// levelTally counts one level's occurrences and the distinct errors behind
// them.
type levelTally struct {
	occurrences int
	distinct    int
}

func (lt *levelTally) add(e types.EnrichedEntry) {
	lt.occurrences += max(e.OccurrenceCount, 1)
	lt.distinct++
}

// describe renders "3000 error", followed by "(N distinct)" when some errors
// repeated.
func (lt levelTally) describe(level string) string {
	if lt.occurrences == lt.distinct {
		return fmt.Sprintf("%d %s", lt.occurrences, level)
	}
	return fmt.Sprintf("%d %s (%d distinct)", lt.occurrences, level, lt.distinct)
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

// formatEntry renders a single EnrichedEntry as a log-like line.
func formatEntry(e types.EnrichedEntry) string {
	var sb strings.Builder

	if e.Timestamp.IsZero() {
		sb.WriteString("-")
	} else {
		sb.WriteString(formatSpan(e.Timestamp, e.LastSeen))
	}
	sb.WriteString(" [")
	sb.WriteString(e.Level.String())
	sb.WriteString("] ")
	if e.Source != "" {
		sb.WriteString(e.Source)
		sb.WriteString(": ")
	}
	sb.WriteString(capMessage(e.Message))

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

// formatSpan renders start, and "–end" when end is later: the end's time
// alone on the same day, its full date otherwise.
func formatSpan(start, end time.Time) string {
	const full = "2006-01-02T15:04:05Z"
	start, end = start.UTC(), end.UTC()
	span := start.Format(full)
	if !end.After(start) {
		return span
	}
	if end.Format(time.DateOnly) == start.Format(time.DateOnly) {
		return span + "–" + end.Format("15:04:05Z")
	}
	return span + "–" + end.Format(full)
}

// capMessage cuts msg to at most maxMessageBytes on a UTF-8 boundary and says
// how much was left out.
func capMessage(msg string) string {
	if len(msg) <= maxMessageBytes {
		return msg
	}
	cut := maxMessageBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return fmt.Sprintf("%s …[%d more bytes]", msg[:cut], len(msg)-cut)
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

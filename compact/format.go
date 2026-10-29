package compact

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oxforge/unlog/types"
)

// Budget fractions for each output section. They sum to 1.0.
const (
	budgetOverview    = 0.05
	budgetDeployments = 0.05
	budgetCritical    = 0.45
	budgetChains      = 0.20
	budgetAnomalies   = 0.10
	budgetContext     = 0.15
)

const (
	sectionOverview    = "Incident Overview"
	sectionDeployments = "Deployments"
	sectionCritical    = "Critical Errors"
	sectionChains      = "Error Chains"
	sectionAnomalies   = "Rate Anomalies"
	sectionContext     = "Context"
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
func formatSummary(all []scoredEntry, bursts []types.Burst, tokenBudget int) string {
	var sb strings.Builder

	claimed := make(map[int]bool)

	// A chain's members, whatever their level, are shown together in it, an
	// entry in two chains in both; chains are ranked by their best-scored
	// member (all is score-ordered).
	var chainIDs []string
	chains := make(map[string][]chainSlot)
	for i, se := range all {
		links := se.entry.ChainLinks()
		for _, link := range links {
			if _, ok := chains[link.ID]; !ok {
				chainIDs = append(chainIDs, link.ID)
			}
			chains[link.ID] = append(chains[link.ID], chainSlot{se.entry, link.Step, link.Steps})
		}
		if len(links) > 0 {
			claimed[i] = true
		}
	}

	// Every deployment is listed under Deployments, chain members too; those
	// below ERROR leave the other sections, while an ERROR-level one (a
	// restart after a crash) stays in Critical Errors as well.
	var deployments []scoredEntry
	for i, se := range all {
		if se.entry.IsDeployment {
			deployments = append(deployments, se)
			if se.entry.Level < types.LevelError {
				claimed[i] = true
			}
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
	overviewText := buildOverview(all, len(bursts))
	writeSection(&sb, sectionOverview, truncateToTokens(overviewText, overviewBudget))

	deploySec := newEntrySection(sectionDeployments, deployments, sectionBudget(tokenBudget, budgetDeployments))
	criticalSec := newEntrySection(sectionCritical, criticals, sectionBudget(tokenBudget, budgetCritical))
	chainSec := newChainSection(sectionChains, chainIDs, chains, sectionBudget(tokenBudget, budgetChains))
	burstSec := newBurstSection(sectionAnomalies, bursts, sectionBudget(tokenBudget, budgetAnomalies))
	contextSec := newEntrySection(sectionContext, contextEntries, sectionBudget(tokenBudget, budgetContext))

	// Leftover budget goes out in this order; the sections print in reading
	// order, deployments first so they read before the errors they precede.
	shareUnusedBudget([]*entrySection{criticalSec, chainSec, deploySec, burstSec, contextSec})
	for _, sec := range []*entrySection{deploySec, criticalSec, chainSec, burstSec, contextSec} {
		sec.write(&sb)
	}

	return strings.TrimRight(sb.String(), "\n")
}

// entrySection is one section of rendered items (entries or bursts), most
// important first, with their budget.
type entrySection struct {
	title  string
	lines  []string    // one rendered item each; a burst spans several lines
	tokens []int       // tokens[i] is the cost of lines[i] with its newline
	at     []time.Time // at[i] is when item i happened, for listing in time order
	budget int
}

func (sec *entrySection) add(text string, at time.Time) {
	sec.lines = append(sec.lines, text)
	sec.tokens = append(sec.tokens, EstimateTokens(text+"\n"))
	sec.at = append(sec.at, at)
}

func newEntrySection(title string, items []scoredEntry, budget int) *entrySection {
	sec := &entrySection{title: title, budget: budget}
	for _, se := range items {
		sec.add(formatEntry(se.entry), se.entry.Timestamp)
	}
	return sec
}

// chainSlot is an entry shown in a chain, with the step it matched there.
type chainSlot struct {
	entry       types.EnrichedEntry
	step, steps int
}

// newChainSection lists each chain as one item, in the order of ids.
func newChainSection(title string, ids []string, chains map[string][]chainSlot, budget int) *entrySection {
	sec := &entrySection{title: title, budget: budget}
	for _, id := range ids {
		text, start := formatChain(id, chains[id])
		sec.add(text, start)
	}
	return sec
}

// newBurstSection ranks bursts by event count (then start, then source) so
// the largest survive a short budget.
func newBurstSection(title string, bursts []types.Burst, budget int) *entrySection {
	ranked := append([]types.Burst(nil), bursts...)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Events != ranked[j].Events {
			return ranked[i].Events > ranked[j].Events
		}
		if !ranked[i].Start.Equal(ranked[j].Start) {
			return ranked[i].Start.Before(ranked[j].Start)
		}
		return ranked[i].Source < ranked[j].Source
	})
	sec := &entrySection{title: title, budget: budget}
	for _, b := range ranked {
		sec.add(formatBurst(b), b.Start)
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

	if len(sec.lines) == 0 {
		sb.WriteString("(none)\n\n")
		return
	}

	var chosen []int
	used := 0
	for i := range sec.lines {
		if used+sec.tokens[i] > sec.budget && len(chosen) > 0 {
			break
		}
		chosen = append(chosen, i)
		used += sec.tokens[i]
	}

	sort.SliceStable(chosen, func(a, b int) bool {
		ta, tb := sec.at[chosen[a]], sec.at[chosen[b]]
		if ta.IsZero() != tb.IsZero() {
			return tb.IsZero()
		}
		return ta.Before(tb)
	})
	for _, i := range chosen {
		sb.WriteString(sec.lines[i])
		sb.WriteString("\n")
	}
	if omitted := len(sec.lines) - len(chosen); omitted > 0 {
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
func buildOverview(all []scoredEntry, burstCount int) string {
	if len(all) == 0 {
		return "No significant log entries found."
	}

	var earliest, latest time.Time
	var fatal, errs, warn levelTally
	sources := make(map[string]struct{})
	var chainIDs []string
	chainSeen := make(map[string]struct{})

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
		// Each occurrence counts at its own level; an entry without the
		// per-level breakdown counts at its level.
		byLevel := e.OccurrencesByLevel
		if len(byLevel) == 0 {
			byLevel = map[types.Level]int{e.Level: max(e.OccurrenceCount, 1)}
		}
		for lvl, n := range byLevel {
			switch lvl {
			case types.LevelFatal:
				fatal.add(n)
			case types.LevelError:
				errs.add(n)
			case types.LevelWarn:
				warn.add(n)
			}
		}
		if e.Source != "" {
			sources[e.Source] = struct{}{}
		}
		for _, link := range e.ChainLinks() {
			if _, seen := chainSeen[link.ID]; !seen {
				chainSeen[link.ID] = struct{}{}
				chainIDs = append(chainIDs, link.ID)
			}
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
	if line := lastDeploymentLine(all); line != "" {
		parts = append(parts, line)
	}

	if len(chainIDs) > 0 {
		parts = append(parts, fmt.Sprintf("Error chains detected: %s", strings.Join(chainIDs, ", ")))
	}
	if burstCount > 0 {
		parts = append(parts, fmt.Sprintf("Bursts: %d", burstCount))
	}

	return strings.Join(parts, "\n")
}

// lastDeploymentLine names the latest deployment at or before the first
// error, and how long before it; "" when there is none.
func lastDeploymentLine(all []scoredEntry) string {
	var firstError time.Time
	for _, se := range all {
		e := se.entry
		if e.Level >= types.LevelError && !e.Timestamp.IsZero() &&
			(firstError.IsZero() || e.Timestamp.Before(firstError)) {
			firstError = e.Timestamp
		}
	}
	if firstError.IsZero() {
		return ""
	}
	var last time.Time
	for _, se := range all {
		e := se.entry
		if !e.IsDeployment {
			continue
		}
		for _, t := range []time.Time{e.Timestamp, e.LastSeen} {
			if !t.IsZero() && !t.After(firstError) && t.After(last) {
				last = t
			}
		}
	}
	if last.IsZero() {
		return ""
	}
	return fmt.Sprintf("Last deployment: %s, %s before the first error",
		last.UTC().Format(time.RFC3339), firstError.Sub(last).Round(time.Second))
}

// levelTally counts one level's occurrences and the distinct errors behind
// them.
type levelTally struct {
	occurrences int
	distinct    int
}

// add records one distinct error with n occurrences at this level.
func (lt *levelTally) add(n int) {
	lt.occurrences += n
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
	if origin := e.Origin(); origin != "" {
		sb.WriteString(origin)
		sb.WriteString(": ")
	}
	sb.WriteString(capMessage(e.Message))

	var tags []string
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
	if len(e.CorrelatedWith) > 0 {
		tags = append(tags, "with="+joinCapped(e.CorrelatedWith, max(e.CorrelatedCount, len(e.CorrelatedWith)), 2))
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

// formatChain renders a chain: a header with how many of its steps occurred
// and its span, then its members in time order, each numbered with the step
// it matched.
func formatChain(id string, members []chainSlot) (string, time.Time) {
	sorted := append([]chainSlot(nil), members...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].entry.Timestamp.Equal(sorted[j].entry.Timestamp) {
			return sorted[i].entry.Timestamp.Before(sorted[j].entry.Timestamp)
		}
		return sorted[i].step < sorted[j].step
	})
	start, end := sorted[0].entry.Timestamp, sorted[0].entry.Timestamp
	steps := make(map[int]bool)
	for _, m := range sorted {
		end = maxTime(end, maxTime(m.entry.Timestamp, m.entry.LastSeen))
		steps[m.step] = true
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: %d of %d steps, %s", id, len(steps), sorted[0].steps, formatSpan(start, end))
	for _, m := range sorted {
		fmt.Fprintf(&sb, "\n  %d. %s", m.step, formatEntry(m.entry))
	}
	return sb.String(), start
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// formatBurst renders a burst: a header with its span, source, size and
// normal rate, then one indented line per top error.
func formatBurst(b types.Burst) string {
	dur := b.End.Sub(b.Start) + time.Second
	normal := "(normally quiet)"
	if b.Baseline > 0 {
		rate := float64(b.Events) / dur.Seconds()
		normal = fmt.Sprintf("(normal ~%.1f/s, %d× above)", b.Baseline, int(math.Round(rate/b.Baseline)))
	}
	var sb strings.Builder
	origin := types.LogEntry{Source: b.Source, Service: b.Service}.Origin()
	fmt.Fprintf(&sb, "%s %s: %d events in %s %s", formatSpan(b.Start, b.End), origin, b.Events, dur, normal)
	for _, te := range b.TopErrors {
		sb.WriteString("\n  ")
		sb.WriteString(capMessage(te.Message))
		if te.Count > 1 {
			fmt.Fprintf(&sb, " {×%d}", te.Count)
		}
	}
	return sb.String()
}

// joinCapped joins up to limit items with commas and counts the rest
// ("a,b,c,+2").
func joinCapped(items []string, total, limit int) string {
	shown := items[:min(len(items), limit)]
	if total <= len(shown) {
		return strings.Join(shown, ",")
	}
	return strings.Join(shown, ",") + fmt.Sprintf(",+%d", total-len(shown))
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

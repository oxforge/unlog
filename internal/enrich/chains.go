package enrich

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/oxforge/unlog/internal/types"
)

// chainPattern defines a sequence of log events that form a known error chain.
type chainPattern struct {
	Name   string
	Window time.Duration
	Stages []chainStage
}

// chainStage defines one step in an error chain pattern: a message pattern,
// or a deployment as LooksLikeDeployment sees it, at MinLevel or above.
type chainStage struct {
	MessagePattern *regexp.Regexp
	Deployment     bool
	MinLevel       types.Level
}

// ChainMatcher detects error chain patterns across a run's entries.
type ChainMatcher struct {
	patterns []*chainPattern
}

// NewChainMatcher creates a ChainMatcher with the built-in patterns.
func NewChainMatcher() *ChainMatcher {
	return &ChainMatcher{
		patterns: builtinChainPatterns,
	}
}

func (cm *ChainMatcher) matchesStage(entry *types.EnrichedEntry, stage chainStage) bool {
	if stage.MinLevel != types.LevelUnknown && !entry.Level.Meets(stage.MinLevel) {
		return false
	}
	if stage.Deployment {
		return (entry.IsDeployment || LooksLikeDeployment(entry.LogEntry)) && !reRollback.MatchString(entry.Message)
	}
	return stage.MessagePattern.MatchString(entry.Message)
}

// builtinChainPatterns defines the 10 built-in error chain patterns.
var builtinChainPatterns = []*chainPattern{
	{
		Name:   "db-connection-exhaustion",
		Window: 5 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(deadlock|lock\s+timeout|lock\s+wait)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(connection\s+pool\s+exhausted|no\s+available\s+connections|too\s+many\s+connections)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(query\s+failed|cannot\s+execute|database\s+error|sql\s+error)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "oom-cascade",
		Window: 3 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(memory\s+(warning|threshold|pressure|limit)|heap\s+(usage|size)\s*(>|exceed))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(OOM\s*kill|out\s+of\s+memory|memory\s+limit\s+exceeded|killed\s+process)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(pod\s+restart|service\s+unavailable|container\s+(restart|crash)|exit\s+code\s+137)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "deployment-failure",
		Window: 10 * time.Minute,
		Stages: []chainStage{
			// A deployment other than a rollback: the deploy detector's
			// patterns, which also keep these INFO lines past the level filter.
			{Deployment: true, MinLevel: types.LevelInfo},
			{MessagePattern: regexp.MustCompile(`(?i)(health\s*check\s+(fail|timeout|unhealthy)|readiness\s+probe\s+failed|liveness\s+probe\s+failed)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(rollback|roll\s+back|reverting|deployment\s+failed|undo)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "circuit-breaker",
		Window: 2 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(timeout|connection\s+refused|connect\s+failed|ECONNREFUSED)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(circuit\s+(open|breaker\s+open|breaker\s+tripped))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(fallback\s+(activated|triggered|used)|degraded\s+mode)`), MinLevel: types.LevelWarn},
		},
	},
	{
		Name:   "disk-full",
		Window: 5 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(disk\s+space\s+(warning|low|critical)|filesystem\s+(full|usage).*\d{2,3}%)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(write\s+fail|no\s+space\s+left|ENOSPC|cannot\s+write|disk\s+full)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(crash|exit|abort|fatal|service\s+(stopped|terminated))`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "certificate-expiry",
		Window: 5 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(certificate?\s+(expir|near\s+expiry|will\s+expire)|TLS\s+warning|cert\s+renewal)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(TLS\s+handshake\s+(fail|error)|SSL\s+error|certificate?\s+(invalid|expired|verify\s+fail))`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(connection\s+refused|connection\s+closed|cannot\s+connect|ECONNREFUSED)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "dns-failure",
		Window: 3 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(DNS\s+(timeout|resolution\s+fail|lookup\s+fail|NXDOMAIN)|name\s+resolution\s+fail)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(connection\s+failed|cannot\s+connect|dial\s+(tcp|error)|ECONNREFUSED)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(upstream\s+(unavailable|error|timeout)|service\s+unavailable|bad\s+gateway|502|503)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "rate-limiting",
		Window: 5 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(rate\s+limit\s+(warning|approaching|threshold)|throttl(e|ing))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(429|too\s+many\s+requests|rate\s+limit\s+(exceeded|hit))`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(client\s+error|request\s+failed|service\s+degraded|dropped\s+request)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "queue-backlog",
		Window: 10 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(consumer\s+lag|processing\s+behind|queue\s+(depth|size)\s*(>|exceed|growing))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(queue\s+full|buffer\s+(full|overflow)|message\s+(dropped|rejected))`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(producer\s+blocked|publish\s+failed|backpressure|send\s+timeout)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "cascade-failure",
		Window: 5 * time.Minute,
		Stages: []chainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(failed|error|exception).*service`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(timeout|timed?\s+out).*upstream`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(HTTP\s+5\d{2}|status[=: ]+5\d{2}|5\d{2}\s+(error|response))`), MinLevel: types.LevelError},
		},
	},
}

// Chain is a detected error chain: the entries that matched at least two
// of a pattern's stages, in order, within its window, in time order.
type Chain struct {
	ID      string // the pattern name, then "-2", "-3"... for later chains of it
	Pattern string
	Steps   int // the pattern's number of stages
	Matched int // how many of the stages its members matched
	Members []ChainMember
}

// ChainMember is one entry of a chain and the stage (1-based) it matched.
type ChainMember struct {
	Index int // into the entries passed to FindChains
	Step  int
}

// FindChains finds the chains in entries, in any order; undated entries are
// skipped. An entry may belong to chains of several patterns. For each
// pattern, one chain is open at a time:
//
//   - any entry matching a stage opens one, and an entry matching a later
//     stage advances it; it is a chain once two different errors matched
//     two stages in order (the lower one first, or in the same second);
//   - until then, it keeps only the entries within the pattern's window of
//     the newest, so an unrelated earlier match can't hide a chain;
//   - it begins at the earliest error a higher stage followed: later stages'
//     entries that ended before it, and lower stages' entries after it, are
//     not part of it, so its timeline never runs backwards;
//   - once a chain, it takes entries matching its stages up to the pattern's
//     window after its latest new stage first occurred, then closes.
//
// A repeated error is one entry that occurred from FirstSeen to LastSeen;
// while a chain forms, it counts as happening at any time in that span.
// Chains are sorted by their first member's time, then by pattern order.
func (cm *ChainMatcher) FindChains(entries []types.EnrichedEntry) []Chain {
	var order []int
	for i := range entries {
		if !entries[i].Timestamp.IsZero() {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return entries[order[a]].Timestamp.Before(entries[order[b]].Timestamp)
	})

	var chains []Chain
	for _, p := range cm.patterns {
		chains = append(chains, cm.patternChains(p, entries, order)...)
	}
	sort.SliceStable(chains, func(i, j int) bool {
		return entries[chains[i].Members[0].Index].Timestamp.Before(entries[chains[j].Members[0].Index].Timestamp)
	})
	seen := make(map[string]int)
	for i := range chains {
		seen[chains[i].Pattern]++
		chains[i].ID = chains[i].Pattern
		if n := seen[chains[i].Pattern]; n > 1 {
			chains[i].ID = fmt.Sprintf("%s-%d", chains[i].Pattern, n)
		}
	}
	return chains
}

// span is when entry e occurred: from its first to its last occurrence.
func span(e *types.EnrichedEntry) (time.Time, time.Time) {
	if e.LastSeen.After(e.Timestamp) {
		return e.Timestamp, e.LastSeen
	}
	return e.Timestamp, e.Timestamp
}

// openChain is a pattern's open chain, or a candidate for one until two
// different errors matched two of its stages in order.
type openChain struct {
	members []openMember
	steps   []int // steps[s] counts the members of stage s (1-based)
	formed  bool
	start   openMember // once formed: the member it begins at
	until   time.Time  // once formed: the last time it takes entries
}

type openMember struct {
	ChainMember
	first, last time.Time
	sig         string // what makes two members the same error
}

func newOpenChain(stages int) *openChain {
	return &openChain{steps: make([]int, stages+1)}
}

func (c *openChain) add(m openMember) {
	c.members = append(c.members, m)
	c.steps[m.Step]++
}

// top is the highest stage (1-based) any member matched.
func (c *openChain) top() int {
	for s := len(c.steps) - 1; s > 0; s-- {
		if c.steps[s] > 0 {
			return s
		}
	}
	return 0
}

// formsWith reports whether m and a member already there are two different
// errors at two stages in order. Members came no later than m, so a lower
// one is in order, and a higher one only from the same second.
func (c *openChain) formsWith(m openMember) bool {
	other := false
	for s := 1; s < len(c.steps); s++ {
		other = other || (s != m.Step && c.steps[s] > 0)
	}
	if !other {
		return false // every member is at m's stage: the usual storm
	}
	for _, o := range c.members {
		if o.sig != m.sig && (o.Step < m.Step || (o.Step > m.Step && o.first.Equal(m.first))) {
			return true
		}
	}
	return false
}

// dropBefore removes the members that last occurred before cutoff.
func (c *openChain) dropBefore(cutoff time.Time) {
	kept := c.members[:0]
	for _, m := range c.members {
		if m.last.Before(cutoff) {
			c.steps[m.Step]--
			continue
		}
		kept = append(kept, m)
	}
	c.members = kept
}

// begin settles where a newly formed chain begins: the earliest member that
// a different error of a higher stage followed. It drops lower stages'
// members after it, and higher stages' members that ended before it.
func (c *openChain) begin() {
	sorted := append([]openMember(nil), c.members...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].first.Equal(sorted[j].first) {
			return sorted[i].first.Before(sorted[j].first)
		}
		return sorted[i].Step < sorted[j].Step
	})
	for _, cand := range sorted {
		followed := false
		for _, o := range c.members {
			if o.Step > cand.Step && o.sig != cand.sig && !o.first.Before(cand.first) {
				followed = true
				break
			}
		}
		if followed {
			c.start = cand
			break
		}
	}
	kept := c.members[:0]
	for _, m := range c.members {
		if m.Step < c.start.Step || (m.Step > c.start.Step && m.last.Before(c.start.first)) {
			c.steps[m.Step]--
			continue
		}
		kept = append(kept, m)
	}
	c.members = kept
}

// patternChains runs one pattern over entries, visited in order.
func (cm *ChainMatcher) patternChains(p *chainPattern, entries []types.EnrichedEntry, order []int) []Chain {
	var chains []Chain
	var open *openChain
	finish := func() {
		if open != nil && open.formed {
			c := Chain{Pattern: p.Name, Steps: len(p.Stages)}
			for _, m := range open.members {
				c.Members = append(c.Members, m.ChainMember)
			}
			for s := 1; s < len(open.steps); s++ {
				if open.steps[s] > 0 {
					c.Matched++
				}
			}
			chains = append(chains, c)
		}
		open = nil
	}
	for _, i := range order {
		e := &entries[i]
		var matched []int // the stages (1-based) e matches
		for s := range p.Stages {
			if cm.matchesStage(e, p.Stages[s]) {
				matched = append(matched, s+1)
			}
		}
		if len(matched) == 0 {
			continue
		}
		first, last := span(e)

		if open != nil && open.formed && first.After(open.until) {
			finish()
		}
		if open != nil && !open.formed {
			open.dropBefore(first.Add(-p.Window))
			if len(open.members) == 0 {
				open = nil
			}
		}
		if open == nil {
			open = newOpenChain(len(p.Stages))
		}

		// Take the lowest stage above the chain's top, which advances it;
		// else the highest one at or below it, a repeat.
		top := open.top()
		step := 0
		for _, s := range matched {
			if s > top {
				step = s
				break
			}
		}
		if step == 0 {
			step = matched[len(matched)-1]
		}
		sig := e.Signature
		if sig == "" {
			sig = e.Message
		}
		m := openMember{ChainMember{Index: i, Step: step}, first, last, sig}

		switch {
		case !open.formed:
			forms := open.formsWith(m)
			open.add(m)
			if forms {
				open.formed = true
				open.begin()
				open.until = first.Add(p.Window)
			}
		case step < open.start.Step:
			// Before the chain's first step; in order only in the same second.
			if first.Equal(open.start.first) {
				open.add(m)
				open.start = m
			}
		default:
			open.add(m)
			if step > top {
				open.until = maxTime(open.until, first.Add(p.Window))
			}
		}
	}
	finish()
	return chains
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

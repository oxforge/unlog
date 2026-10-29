package enrich

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/oxforge/unlog/types"
)

// chainPattern defines a sequence of log events that form a known error chain.
type chainPattern struct {
	Name   string
	Window time.Duration
	Stages []chainStage
}

// chainStage defines one step in an error chain pattern.
type chainStage struct {
	SourcePattern  *regexp.Regexp // nil = any source
	MessagePattern *regexp.Regexp
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
	if stage.SourcePattern != nil && !stage.SourcePattern.MatchString(entry.Source) {
		return false
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
			{MessagePattern: regexp.MustCompile(`(?i)(deploy(ing|ment)\s+start|rolling\s+update|new\s+version|pulling\s+image)`), MinLevel: types.LevelInfo},
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

// Chain is a detected error chain: the entries that matched at least two of a
// pattern's stages within its window, in time order.
type Chain struct {
	ID      string // the pattern name, then "-2", "-3"... for later chains of it
	Pattern string
	Steps   int // the pattern's number of stages
	Matched int // the highest stage reached
	Members []ChainMember
}

// ChainMember is one entry of a chain and the stage (1-based) it matched.
type ChainMember struct {
	Index int // into the entries passed to FindChains
	Step  int
}

// FindChains finds the chains in entries, which must be in time order.
// Undated entries are skipped. For each pattern, one chain is open at a time:
// it starts at an entry matching the first stage, takes the next stage when
// an entry matches it within the pattern's window, and takes further entries
// matching any stage it has already reached (more timeouts join the same
// chain). A chain that reached at least two stages is reported. Chains are
// sorted by their first member, then by pattern order.
func (cm *ChainMatcher) FindChains(entries []types.EnrichedEntry) []Chain {
	var chains []Chain
	for _, p := range cm.patterns {
		chains = append(chains, cm.patternChains(p, entries)...)
	}
	sort.SliceStable(chains, func(i, j int) bool {
		return chains[i].Members[0].Index < chains[j].Members[0].Index
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

// patternChains runs one pattern over entries.
func (cm *ChainMatcher) patternChains(p *chainPattern, entries []types.EnrichedEntry) []Chain {
	var chains []Chain
	var open *Chain
	var openedAt time.Time
	finish := func() {
		if open != nil && open.Matched >= 2 {
			chains = append(chains, *open)
		}
		open = nil
	}
	for i := range entries {
		e := &entries[i]
		if e.Timestamp.IsZero() {
			continue
		}
		if open != nil && e.Timestamp.Sub(openedAt) > p.Window {
			finish()
		}
		if open == nil {
			if cm.matchesStage(e, p.Stages[0]) {
				open = &Chain{Pattern: p.Name, Steps: len(p.Stages), Matched: 1,
					Members: []ChainMember{{Index: i, Step: 1}}}
				openedAt = e.Timestamp
			}
			continue
		}
		if cm.matchesStage(e, p.Stages[open.Matched]) {
			open.Matched++
			open.Members = append(open.Members, ChainMember{Index: i, Step: open.Matched})
			if open.Matched == len(p.Stages) {
				finish()
			}
			continue
		}
		for s := open.Matched - 1; s >= 0; s-- {
			if cm.matchesStage(e, p.Stages[s]) {
				open.Members = append(open.Members, ChainMember{Index: i, Step: s + 1})
				break
			}
		}
	}
	finish()
	return chains
}

package enrich

import (
	"fmt"
	"regexp"
	"time"

	"github.com/oxforge/unlog/types"
)

// ChainPattern defines a sequence of log events that form a known error chain.
type ChainPattern struct {
	Name   string
	Window time.Duration
	Stages []ChainStage
}

// ChainStage defines one step in an error chain pattern.
type ChainStage struct {
	SourcePattern  *regexp.Regexp // nil = any source
	MessagePattern *regexp.Regexp
	MinLevel       types.Level
}

// activeChain tracks a partially matched chain pattern.
type activeChain struct {
	Pattern   *ChainPattern
	ID        string
	StartTime time.Time
	NextStage int
	EntryIDs  []int64
}

// maxActiveChainsPerPattern limits how many active chains a single pattern
// can have simultaneously, preventing unbounded growth during error spikes.
const maxActiveChainsPerPattern = 100

// ChainMatcher detects error chain patterns across a stream of log entries.
type ChainMatcher struct {
	patterns []*ChainPattern
	active   []*activeChain
	maxTime  time.Time // high-water mark for expiration
	counter  int64     // per-instance chain ID counter
}

// NewChainMatcher creates a ChainMatcher with the built-in patterns.
func NewChainMatcher() *ChainMatcher {
	return &ChainMatcher{
		patterns: builtinChainPatterns,
	}
}

// Match checks the entry against active and potential chains.
func (cm *ChainMatcher) Match(entry *types.EnrichedEntry) string {
	ts := entry.Timestamp
	if ts.After(cm.maxTime) {
		cm.maxTime = ts
	}

	cm.expire()

	var matchedChainID string

	for _, ac := range cm.active {
		if ac.NextStage >= len(ac.Pattern.Stages) {
			continue
		}
		stage := ac.Pattern.Stages[ac.NextStage]
		if cm.matchesStage(entry, stage) {
			ac.NextStage++
			ac.EntryIDs = append(ac.EntryIDs, entry.LineNumber)
			if ac.NextStage >= len(ac.Pattern.Stages) {
				matchedChainID = ac.ID
			} else if matchedChainID == "" {
				matchedChainID = ac.ID
			}
		}
	}

	for _, p := range cm.patterns {
		stage := p.Stages[0]
		if cm.matchesStage(entry, stage) {
			if cm.countActiveForPattern(p.Name) >= maxActiveChainsPerPattern {
				continue
			}
			cm.counter++
			id := fmt.Sprintf("%s-%d", p.Name, cm.counter)
			ac := &activeChain{
				Pattern:   p,
				ID:        id,
				StartTime: ts,
				NextStage: 1,
				EntryIDs:  []int64{entry.LineNumber},
			}
			cm.active = append(cm.active, ac)
			if matchedChainID == "" {
				matchedChainID = id
			}
		}
	}

	return matchedChainID
}

func (cm *ChainMatcher) matchesStage(entry *types.EnrichedEntry, stage ChainStage) bool {
	if stage.MinLevel != types.LevelUnknown && !entry.Level.Meets(stage.MinLevel) {
		return false
	}
	if stage.SourcePattern != nil && !stage.SourcePattern.MatchString(entry.Source) {
		return false
	}
	return stage.MessagePattern.MatchString(entry.Message)
}

func (cm *ChainMatcher) countActiveForPattern(name string) int {
	count := 0
	for _, ac := range cm.active {
		if ac.Pattern.Name == name {
			count++
		}
	}
	return count
}

func (cm *ChainMatcher) expire() {
	n := 0
	for _, ac := range cm.active {
		if cm.maxTime.Sub(ac.StartTime) <= ac.Pattern.Window {
			cm.active[n] = ac
			n++
		}
	}
	for i := n; i < len(cm.active); i++ {
		cm.active[i] = nil
	}
	cm.active = cm.active[:n]
}

// builtinChainPatterns defines the 10 built-in error chain patterns.
var builtinChainPatterns = []*ChainPattern{
	{
		Name:   "db-connection-exhaustion",
		Window: 5 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(deadlock|lock\s+timeout|lock\s+wait)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(connection\s+pool\s+exhausted|no\s+available\s+connections|too\s+many\s+connections)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(query\s+failed|cannot\s+execute|database\s+error|sql\s+error)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "oom-cascade",
		Window: 3 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(memory\s+(warning|threshold|pressure|limit)|heap\s+(usage|size)\s*(>|exceed))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(OOM\s*kill|out\s+of\s+memory|memory\s+limit\s+exceeded|killed\s+process)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(pod\s+restart|service\s+unavailable|container\s+(restart|crash)|exit\s+code\s+137)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "deployment-failure",
		Window: 10 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(deploy(ing|ment)\s+start|rolling\s+update|new\s+version|pulling\s+image)`), MinLevel: types.LevelInfo},
			{MessagePattern: regexp.MustCompile(`(?i)(health\s*check\s+(fail|timeout|unhealthy)|readiness\s+probe\s+failed|liveness\s+probe\s+failed)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(rollback|roll\s+back|reverting|deployment\s+failed|undo)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "circuit-breaker",
		Window: 2 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(timeout|connection\s+refused|connect\s+failed|ECONNREFUSED)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(circuit\s+(open|breaker\s+open|breaker\s+tripped))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(fallback\s+(activated|triggered|used)|degraded\s+mode)`), MinLevel: types.LevelWarn},
		},
	},
	{
		Name:   "disk-full",
		Window: 5 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(disk\s+space\s+(warning|low|critical)|filesystem\s+(full|usage).*\d{2,3}%)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(write\s+fail|no\s+space\s+left|ENOSPC|cannot\s+write|disk\s+full)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(crash|exit|abort|fatal|service\s+(stopped|terminated))`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "certificate-expiry",
		Window: 5 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(certificate?\s+(expir|near\s+expiry|will\s+expire)|TLS\s+warning|cert\s+renewal)`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(TLS\s+handshake\s+(fail|error)|SSL\s+error|certificate?\s+(invalid|expired|verify\s+fail))`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(connection\s+refused|connection\s+closed|cannot\s+connect|ECONNREFUSED)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "dns-failure",
		Window: 3 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(DNS\s+(timeout|resolution\s+fail|lookup\s+fail|NXDOMAIN)|name\s+resolution\s+fail)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(connection\s+failed|cannot\s+connect|dial\s+(tcp|error)|ECONNREFUSED)`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(upstream\s+(unavailable|error|timeout)|service\s+unavailable|bad\s+gateway|502|503)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "rate-limiting",
		Window: 5 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(rate\s+limit\s+(warning|approaching|threshold)|throttl(e|ing))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(429|too\s+many\s+requests|rate\s+limit\s+(exceeded|hit))`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(client\s+error|request\s+failed|service\s+degraded|dropped\s+request)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "queue-backlog",
		Window: 10 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(consumer\s+lag|processing\s+behind|queue\s+(depth|size)\s*(>|exceed|growing))`), MinLevel: types.LevelWarn},
			{MessagePattern: regexp.MustCompile(`(?i)(queue\s+full|buffer\s+(full|overflow)|message\s+(dropped|rejected))`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(producer\s+blocked|publish\s+failed|backpressure|send\s+timeout)`), MinLevel: types.LevelError},
		},
	},
	{
		Name:   "cascade-failure",
		Window: 5 * time.Minute,
		Stages: []ChainStage{
			{MessagePattern: regexp.MustCompile(`(?i)(failed|error|exception).*service`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(timeout|timed?\s+out).*upstream`), MinLevel: types.LevelError},
			{MessagePattern: regexp.MustCompile(`(?i)(HTTP\s+5\d{2}|status[=: ]+5\d{2}|5\d{2}\s+(error|response))`), MinLevel: types.LevelError},
		},
	},
}

package enrich

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/oxforge/unlog/types"
)

// deployPatterns recognize deployment, restart, scaling and rollback lines;
// all case-insensitive. Words that also occur elsewhere need their context:
// a version or a restart is checked against the word before it (see
// notAppVersion and notRestarts; a pod still "restarting" is a symptom),
// scaling names replicas, a rollout or rollback names a version or
// deployment.
var deployPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)starting\s+(application|service|server)`),
	regexp.MustCompile(`(?i)listening\s+on\s+port`),
	regexp.MustCompile(`(?i)(deployed|deploying|deployment)\s+(version|v\d)`),
	regexp.MustCompile(`(?i)rolling\s+update`),
	regexp.MustCompile(`(?i)container\s+started`),
	regexp.MustCompile(`(?i)migration\s+(applied|running|complete)`),
	regexp.MustCompile(`(?i)\brestart\s+(complete|successful)\b`),
	regexp.MustCompile(`(?i)pulling\s+image`),
	regexp.MustCompile(`(?i)\bscal(ed|ing)\s+(up|down|out|in|deployment|statefulset|replicaset)\b`),
	regexp.MustCompile(`(?i)\bscal(ed|ing)\s+(\S+\s+)?to\s+\d+\s+(replicas|instances|pods|nodes)\b`),
	regexp.MustCompile(`(?i)deploy(ment)?\s+(started|starting|initiated|in\s+progress)`),
	regexp.MustCompile(`(?i)\b(rollout\s+(of\s+\S+\s+)?(started|complete|completed|finished|status|restart|updated|paused|resumed|aborted|promoted)|successfully\s+rolled\s+out|rolling\s+out\s+(\S+\s+)?(v?\d+\.\d|version|release|deployment|image))`),
	regexp.MustCompile(`(?i)\bnew\s+version\s+v?\d`),
	regexp.MustCompile(`(?i)\b(rollback|rolling\s+back|rolled\s+back)\b.*\b(deploy|deployment|release|version|v\d)`),
}

// A restart or a version names a deployment unless the word before it says
// it is something else: a "retry timer restarted", a "TLS version: 1.0".
var (
	reRestarted   = regexp.MustCompile(`(?i)(?:^|(\S+)\s+)restarted\b`)
	reVersionKey  = regexp.MustCompile(`(?i)(?:^|(\S+)\s+)version\s*[:=]\s*v?\d+\.\d+`)
	notRestarts   = wordSet("timer retry retries connection connections watch watcher stream backoff loop session transaction query request iteration counter scan poll polling cycle heartbeat")
	notAppVersion = wordSet("protocol tls ssl http https api schema client driver kernel openssl library lib sdk runtime java python node go browser agent")
)

func wordSet(words string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// matchesUnless reports whether re matches msg with its captured word before
// the match absent (the match starts the line) or not in deny.
func matchesUnless(re *regexp.Regexp, deny map[string]bool, msg string) bool {
	for _, m := range re.FindAllStringSubmatch(msg, -1) {
		if !deny[strings.ToLower(strings.TrimRight(m[1], ":,"))] {
			return true
		}
	}
	return false
}

// reRollback finds a rollback, which is a deployment marker but ends a
// deployment-failure chain rather than starting one.
var reRollback = regexp.MustCompile(`(?i)\b(rollback|rolling\s+back|rolled\s+back|reverting)\b`)

// deployKeywords is a cheap first check: a message none of them occurs in
// (lowercased) matches no deploy pattern, and most lines are like that.
var deployKeywords = []string{
	"start", "listen", "deploy", "rolling", "container", "migration", "version",
	"restart", "pulling", "scal", "rollout", "rolled", "rollback",
}

// deployMetadataKeys are metadata keys that may indicate deployment events.
var deployMetadataKeys = []string{"event", "action", "type"}

// deployMetadataWords are the words of such a value ("deploy.started",
// "scale-up") that indicate a deployment event.
var deployMetadataWords = map[string]bool{
	"deploy": true, "deployment": true, "deployed": true, "redeploy": true, "redeployed": true,
	"restart": true, "restarted": true, "rollout": true, "rollback": true,
	"scale": true, "scaled": true, "scaling": true, "autoscale": true, "autoscaled": true, "autoscaling": true,
}

// DeployDetector detects deployment and restart events in log entries.
type DeployDetector struct{}

// NewDeployDetector creates a new DeployDetector.
func NewDeployDetector() *DeployDetector {
	return &DeployDetector{}
}

// Detect sets IsDeployment when the entry looks like a deployment.
func (d *DeployDetector) Detect(entry *types.EnrichedEntry) {
	if LooksLikeDeployment(entry.LogEntry) {
		entry.IsDeployment = true
	}
}

// LooksLikeDeployment reports whether entry is a deployment, restart or
// scaling event: an event/action/type field says so, or the message matches a
// deployment pattern.
func LooksLikeDeployment(entry types.LogEntry) bool {
	for _, key := range deployMetadataKeys {
		if v, ok := entry.Metadata[key]; ok {
			for _, w := range metadataWords(v) {
				if deployMetadataWords[w] {
					return true
				}
			}
		}
	}
	lower := strings.ToLower(entry.Message)
	if !slices.ContainsFunc(deployKeywords, func(kw string) bool { return strings.Contains(lower, kw) }) {
		return false
	}
	for _, re := range deployPatterns {
		if re.MatchString(entry.Message) {
			return true
		}
	}
	return matchesUnless(reRestarted, notRestarts, entry.Message) ||
		matchesUnless(reVersionKey, notAppVersion, entry.Message)
}

// metadataWords splits a value like "deploy.started", "scale-up" or
// "DeploymentStarted" into its lowercase words.
func metadataWords(v string) []string {
	var sb strings.Builder
	prev := rune(0)
	for _, r := range v {
		if unicode.IsUpper(r) && unicode.IsLower(prev) {
			sb.WriteByte(' ')
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		} else {
			sb.WriteByte(' ')
		}
		prev = r
	}
	return strings.Fields(sb.String())
}

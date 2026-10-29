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
// a version names an app or starts the line ("protocol version=1.1" is not
// one), a restart names what restarted (a pod still "restarting" is a
// symptom), scaling names replicas, a rollout or rollback names a version or
// deployment.
var deployPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)starting\s+(application|service|server)`),
	regexp.MustCompile(`(?i)listening\s+on\s+port`),
	regexp.MustCompile(`(?i)(deployed|deploying|deployment)\s+(version|v\d)`),
	regexp.MustCompile(`(?i)rolling\s+update`),
	regexp.MustCompile(`(?i)container\s+started`),
	regexp.MustCompile(`(?i)migration\s+(applied|running|complete)`),
	regexp.MustCompile(`(?i)(^|\b(app|application|service|build|release|running)\s+)version\s*[:=]\s*v?\d+\.\d+`),
	regexp.MustCompile(`(?i)\b(service|server|pod|container|process|application|app|worker|instance|daemon)\s+(\S+\s+)?restarted\b`),
	regexp.MustCompile(`(?i)\brestart(ed)?\s+(complete|successful|the\s+\w+|service|server|pod|container)\b`),
	regexp.MustCompile(`(?i)pulling\s+image`),
	regexp.MustCompile(`(?i)\bscal(ed|ing)\s+(up|down|out|in|deployment|statefulset|replicaset)\b`),
	regexp.MustCompile(`(?i)\bscal(ed|ing)\s+(\S+\s+)?to\s+\d+\s+(replicas|instances|pods|nodes)\b`),
	regexp.MustCompile(`(?i)deploy(ment)?\s+(started|starting|initiated|in\s+progress)`),
	regexp.MustCompile(`(?i)\b(rollout\s+(of\s+\S+\s+)?(started|complete|completed|finished|status|restart)|successfully\s+rolled\s+out|rolling\s+out\s+(v?\d|version|release|deployment|image))`),
	regexp.MustCompile(`(?i)\bnew\s+version\s+v?\d`),
	regexp.MustCompile(`(?i)\b(rollback|rolling\s+back|rolled\s+back)\b.*\b(deploy|deployment|release|version|v\d)`),
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
	"deploy": true, "deployment": true, "deployed": true, "restart": true, "restarted": true,
	"rollout": true, "rollback": true, "scale": true, "scaled": true, "scaling": true,
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
			words := strings.FieldsFunc(strings.ToLower(v), func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r)
			})
			for _, w := range words {
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
	return false
}

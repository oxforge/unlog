package enrich

import (
	"regexp"
	"strings"

	"github.com/oxforge/unlog/types"
)

// Compiled deployment detection patterns, all case-insensitive.
var deployPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)starting\s+(application|service|server)`),
	regexp.MustCompile(`(?i)listening\s+on\s+port`),
	regexp.MustCompile(`(?i)(deployed|deploying|deployment)\s+(version|v\d)`),
	regexp.MustCompile(`(?i)rolling\s+update`),
	regexp.MustCompile(`(?i)container\s+started`),
	regexp.MustCompile(`(?i)migration\s+(applied|running|complete)`),
	regexp.MustCompile(`(?i)version\s*[:=]\s*v?\d+\.\d+`),
	regexp.MustCompile(`(?i)(restarted|restart\s+complete)`),
	regexp.MustCompile(`(?i)pulling\s+image`),
	regexp.MustCompile(`(?i)(scaling|scaled)\s+(up|down|to)`),
	regexp.MustCompile(`(?i)deploy(ment)?\s+(started|starting|initiated|in\s+progress)`),
	regexp.MustCompile(`(?i)\b(rollout|rolling\s+out)\b`),
	regexp.MustCompile(`(?i)\bnew\s+version\s+v?\d`),
}

// deployMetadataKeys are metadata keys that may indicate deployment events.
var deployMetadataKeys = []string{"event", "action", "type"}

// deployMetadataValues are values in metadata that indicate deployment events.
var deployMetadataValues = []string{"deploy", "restart", "rollout", "scale"}

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
			vLower := strings.ToLower(v)
			for _, dv := range deployMetadataValues {
				if strings.Contains(vLower, dv) {
					return true
				}
			}
		}
	}
	for _, re := range deployPatterns {
		if re.MatchString(entry.Message) {
			return true
		}
	}
	return false
}

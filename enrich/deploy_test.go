package enrich

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestDeployDetector_Patterns(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{"starting application", "Starting application on port 8080", true},
		{"starting server", "Starting server...", true},
		{"listening on port", "Listening on port 3000", true},
		{"deployment version", "Deployed version v2.3.1", true},
		{"deploying version", "Deploying version v1.0", true},
		{"rolling update", "Rolling update in progress", true},
		{"container started", "Container started successfully", true},
		{"migration applied", "Migration applied: 20250101_add_users", true},
		{"migration running", "Migration running: 003_create_index", true},
		{"version colon", "version: v1.2.3", true},
		{"version equals", "version=2.0.0", true},
		{"restarted", "Service restarted", true},
		{"restart complete", "Restart complete for api-server", true},
		{"pulling image", "Pulling image nginx:latest", true},
		{"scaling up", "Scaling up to 5 replicas", true},
		{"scaled down", "Scaled down to 2 instances", true},
		{"no match", "GET /api/users 200 OK", false},
		{"no match error", "Connection refused to database", false},
		{"partial no match", "starting breakfast", false},
		{"app version", "application version: 2.4.1", true},
		{"scaled a deployment", "Scaled deployment api to 5 replicas", true},
		{"pod restarted", "pod api-7d9f restarted", true},
		{"rollback of a release", "rollback initiated: reverting to version 3.2.1", true},
		{"rolling back a deployment", "rolling back deployment", true},
		{"kubectl rollout", `deployment "api" successfully rolled out`, true},
		{"rolling out a version", "rolling out version 2.4.1 to production", true},

		// Look-alikes that aren't deployments.
		{"a protocol version", "http client protocol version=1.1", false},
		{"a TLS version error", "TLS handshake failed: unsupported TLS version: 1.0", false},
		{"a timer restarted", "retry timer restarted", false},
		{"an image scaled", "thumbnail scaled to 200px", false},
		{"a feature flag rollout", "rolling out feature flag new-checkout to 5% of users", false},
		{"a transaction rolled back", "transaction rolled back: deadlock detected", false},
		{"a pod still restarting", "health check degraded: pod restarting", false},

		// Real markers the context rules must not lose.
		{"a program restarted", "nginx restarted", true},
		{"systemd restarted", "Restarted nginx.service", true},
		{"a numbered worker restarted", "worker-3 restarted", true},
		{"restarted with a reason", "payments-api restarted after config change", true},
		{"a version after the app name", "myapp version=2.4.1 starting", true},
		{"an Argo rollout", "Rollout updated to revision 5", true},
		{"rolling out a named release", "Rolling out web 2.4.1", true},
		{"a connection restarted", "watch connection restarted", false},
	}

	dd := NewDeployDetector()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := makeEnrichedEntry(tt.message, nil)
			dd.Detect(&entry)
			assert.Equal(t, tt.want, entry.IsDeployment)
		})
	}
}

func TestDeployDetector_Metadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		want     bool
	}{
		{"event=deploy", map[string]string{"event": "deploy"}, true},
		{"action=restart", map[string]string{"action": "restart"}, true},
		{"type=rollout", map[string]string{"type": "rollout"}, true},
		{"event=scale", map[string]string{"event": "scale-up"}, true},
		{"event=request", map[string]string{"event": "request"}, false},
		{"event=deploy.started", map[string]string{"event": "deploy.started"}, true},
		{"a word containing scale", map[string]string{"event": "rescale_check"}, false},
		{"CamelCase", map[string]string{"event": "DeploymentStarted"}, true},
		{"redeploy", map[string]string{"action": "redeploy"}, true},
		{"autoscale", map[string]string{"type": "autoscale"}, true},
		{"no metadata", nil, false},
	}

	dd := NewDeployDetector()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := makeEnrichedEntry("some message", tt.metadata)
			dd.Detect(&entry)
			assert.Equal(t, tt.want, entry.IsDeployment)
		})
	}
}

func TestDeployDetector_MetadataPriority(t *testing.T) {
	dd := NewDeployDetector()
	// Metadata match should return without checking regex.
	entry := makeEnrichedEntry("no deploy keywords here", map[string]string{"event": "deploy"})
	dd.Detect(&entry)
	assert.True(t, entry.IsDeployment)
}

func TestLooksLikeDeployment(t *testing.T) {
	tests := []struct {
		message string
		meta    map[string]string
		want    bool
	}{
		{"deployment started: app-deployment to version 3.3.0", nil, true},
		{"Deployment initiated for checkout-api", nil, true},
		{"rolling out version 3.3.0 to 3 pods", nil, true},
		{"rollout of api-server complete", nil, true},
		{"rolling update initiated: 3 pods, max-surge=1", nil, true},
		{"build finished", map[string]string{"event": "deploy"}, true},
		{"deployment failure threshold exceeded: initiating rollback", nil, false},
		{"payment declined", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			assert.Equal(t, tt.want, LooksLikeDeployment(types.LogEntry{Message: tt.message, Metadata: tt.meta}))
		})
	}
}

// Every way the deployment-failure chain's first step was worded is a
// deployment, so the line survives the level filter and opens the chain.
func TestLooksLikeDeploymentCoversTheChainStarts(t *testing.T) {
	for _, msg := range []string{
		"deployment starting for api-server v2.4.1",
		"deployment started: app to version 3.3.0",
		"rolling update started",
		"new version 2.4.1 rolled to production",
		"pulling image registry.example.com/api:2.4.1",
	} {
		assert.True(t, LooksLikeDeployment(types.LogEntry{Message: msg}), msg)
	}
}

func BenchmarkLooksLikeDeployment(b *testing.B) {
	entry := types.LogEntry{Message: "GET /api/v1/orders/12345 200 OK in 12ms for 10.0.3.17"}
	for range b.N {
		LooksLikeDeployment(entry)
	}
}

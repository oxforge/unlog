package enrich

import (
	"testing"

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

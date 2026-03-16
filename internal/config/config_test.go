package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oxforge/unlog/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearAllEnv clears all UNLOG_* env vars so tests are isolated from the
// parent process or CI environment.
func clearAllEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"UNLOG_LEVEL", "UNLOG_FORMAT", "UNLOG_NO_COLOR",
		"UNLOG_NOISE_FILE", "UNLOG_SINCE", "UNLOG_UNTIL", "UNLOG_AI_PROVIDER",
		"UNLOG_VERBOSE",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadFromFile(t *testing.T) {
	clearAllEnv(t)
	content := `
level       = "error"
format      = "json"
no_color    = true
noise_file  = "/tmp/noise.txt"
since       = "2h"
until       = "1h"
ai_provider = "openai"
verbose     = true
`
	path := writeTempTOML(t, content)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "error", cfg.Level)
	assert.Equal(t, "json", cfg.Format)
	assert.True(t, cfg.NoColor)
	assert.Equal(t, "/tmp/noise.txt", cfg.NoiseFile)
	assert.Equal(t, "2h", cfg.Since)
	assert.Equal(t, "1h", cfg.Until)
	assert.Equal(t, "openai", cfg.AIProvider)
	assert.True(t, cfg.Verbose)
}

func TestLoadMissingFile(t *testing.T) {
	clearAllEnv(t)
	// A path that definitely does not exist.
	cfg, err := config.Load("/tmp/does-not-exist-unlog-test-config.toml")
	require.NoError(t, err)

	// Zero-value config expected.
	assert.Empty(t, cfg.Level)
	assert.Empty(t, cfg.Format)
	assert.Empty(t, cfg.AIProvider)
}

func TestLoadEmptyPath(t *testing.T) {
	clearAllEnv(t)
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Level)
}

func TestEnvOverlay(t *testing.T) {
	clearAllEnv(t)
	// Write a TOML with some values.
	content := "level = \"warn\"\nformat = \"text\"\n"
	path := writeTempTOML(t, content)

	// Set env vars that should override the file.
	t.Setenv("UNLOG_LEVEL", "error")
	t.Setenv("UNLOG_FORMAT", "json")
	t.Setenv("UNLOG_AI_PROVIDER", "anthropic")

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "error", cfg.Level, "env should override file level")
	assert.Equal(t, "json", cfg.Format, "env should override file format")
	assert.Equal(t, "anthropic", cfg.AIProvider)
}

func TestEnvBoolParsing(t *testing.T) {
	clearAllEnv(t)
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"1", "1", true},
		{"true lowercase", "true", true},
		{"TRUE uppercase", "TRUE", true},
		{"yes", "yes", true},
		{"0", "0", false},
		{"false", "false", false},
		{"no", "no", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearAllEnv(t)
			t.Setenv("UNLOG_NO_COLOR", tc.value)

			cfg, err := config.Load("")
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.NoColor, "UNLOG_NO_COLOR=%q", tc.value)
		})
	}
}

func TestEnvBoolVerbose(t *testing.T) {
	clearAllEnv(t)
	tests := []struct {
		value string
		want  bool
	}{
		{"1", true},
		{"yes", true},
		{"0", false},
		{"false", false},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			clearAllEnv(t)
			t.Setenv("UNLOG_VERBOSE", tc.value)
			cfg, err := config.Load("")
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.Verbose)
		})
	}
}

func TestLoadMalformedTOML(t *testing.T) {
	clearAllEnv(t)
	path := writeTempTOML(t, "this is not valid toml ===")
	_, err := config.Load(path)
	assert.Error(t, err, "malformed TOML should return an error")
}

// writeTempTOML writes content to a temp file and returns its path.
func writeTempTOML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

package hooks_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rule    = "The unlog plugin is active. When you are asked to analyze, triage or find errors in logs (files, directories, archives, or output from kubectl logs, docker compose logs or journalctl), run unlog on them first, following the unlog:analyzing-logs skill, before any grep or ad-hoc script."
	missing = "The hook did not find the unlog CLI on its PATH; if command -v unlog fails in your shell too, tell the user how to install it (https://github.com/oxforge/unlog#installation) and ask before installing anything."
)

// runHook runs session-start with nothing in its environment but a PATH
// (holding a stub unlog when withUnlog is set) and env, checks the JSON it
// prints and returns its additionalContext.
func runHook(t *testing.T, withUnlog bool, env ...string) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}
	bin := t.TempDir()
	if withUnlog {
		require.NoError(t, os.WriteFile(filepath.Join(bin, "unlog"), []byte("#!/bin/sh\n"), 0o755))
	}
	cmd := exec.Command(bash, "session-start")
	cmd.Env = append([]string{"PATH=" + bin}, env...)
	out, err := cmd.Output()
	require.NoError(t, err)
	var got struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	require.NoError(t, json.Unmarshal(out, &got), "output: %s", out)
	require.Equal(t, "SessionStart", got.HookSpecificOutput.HookEventName)
	return got.HookSpecificOutput.AdditionalContext
}

func TestSessionStart_GivesTheRule(t *testing.T) {
	assert.Equal(t, rule, runHook(t, true))
}

func TestSessionStart_NotesAMissingUnlog(t *testing.T) {
	assert.Equal(t, rule+" "+missing, runHook(t, false))
}

func TestHooksJSON_RunsSessionStartOnEveryFreshContext(t *testing.T) {
	raw, err := os.ReadFile("hooks.json")
	require.NoError(t, err)
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(raw, &cfg))
	require.Len(t, cfg.Hooks, 1)
	ss := cfg.Hooks["SessionStart"]
	require.Len(t, ss, 1)
	assert.Equal(t, "startup|clear|compact", ss[0].Matcher)
	require.Len(t, ss[0].Hooks, 1)
	assert.Equal(t, "command", ss[0].Hooks[0].Type)
	assert.Equal(t, `"${CLAUDE_PLUGIN_ROOT}/hooks/session-start"`, ss[0].Hooks[0].Command)
	info, err := os.Stat("session-start")
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111, "the hook runs session-start by its path, so it must stay executable")
}

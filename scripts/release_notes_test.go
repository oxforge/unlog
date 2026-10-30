package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitRepo makes a repository with one commit, isolated from the user's git
// config (which might sign tags), and returns a func that runs git in it.
func gitRepo(t *testing.T) (dir string, git func(args ...string)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir = t.TempDir()
	env := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	git = func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "first commit", "-m", "a commit body that must not become the notes")
	return dir, git
}

// releaseNotes runs scripts/release-notes in dir and returns its stdout,
// stderr and exit code.
func releaseNotes(t *testing.T, dir, tag string) (stdout, stderr string, code int) {
	t.Helper()
	script, err := filepath.Abs("release-notes")
	require.NoError(t, err)
	cmd := exec.Command("bash", script, tag)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return out.String(), errOut.String(), code
}

// The README's tag command keeps Markdown headings, which git's default
// cleanup would strip as comments.
func TestReleaseNotesKeepMarkdownHeadings(t *testing.T) {
	dir, git := gitRepo(t)
	notes := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(notes, []byte("unlog v9.9.9\n\n## Breaking changes\n\n- a flag is gone\n"), 0o644))
	git("tag", "-a", "--cleanup=whitespace", "v9.9.9", "-F", notes)

	out, stderr, code := releaseNotes(t, dir, "v9.9.9")

	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, out, "## Breaking changes\n\n- a flag is gone")
	assert.NotContains(t, out, "unlog v9.9.9", "the first line is the title, not the notes")
}

// A lightweight tag has no message: its notes would be the commit's body.
func TestReleaseNotesRefuseALightweightTag(t *testing.T) {
	dir, git := gitRepo(t)
	git("tag", "v9.9.9")

	out, stderr, code := releaseNotes(t, dir, "v9.9.9")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "v9.9.9 is not an annotated tag")
	assert.Empty(t, out)
}

func TestReleaseNotesRefuseATagWithOnlyATitle(t *testing.T) {
	dir, git := gitRepo(t)
	git("tag", "-a", "v9.9.9", "-m", "unlog v9.9.9")

	_, stderr, code := releaseNotes(t, dir, "v9.9.9")

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "v9.9.9 has no release notes")
}

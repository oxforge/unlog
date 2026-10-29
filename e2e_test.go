package main

// End-to-end tests: they build the real binary once and run it the way a user
// does, with the CLI's own defaults, an isolated HOME (so ~/.unlog/config.toml
// is never the developer's) and only the environment variables a test sets.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oxforge/unlog/types"
)

var unlogBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "unlog-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: temp dir:", err)
		os.Exit(1)
	}
	unlogBin = filepath.Join(dir, "unlog")
	build := exec.Command("go", "build", "-o", unlogBin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: build:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type cliRun struct {
	stdout string
	stderr string
	exit   int
}

// runUnlog runs the binary with HOME set to home and env as the only extra
// environment variables.
func runUnlog(t *testing.T, home string, env []string, args ...string) cliRun {
	t.Helper()
	cmd := exec.Command(unlogBin, args...)
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exit := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		exit = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("run unlog: %v", err)
	}
	return cliRun{stdout: stdout.String(), stderr: stderr.String(), exit: exit}
}

// runJSON runs the binary with --format json and decodes the report.
func runJSON(t *testing.T, args ...string) types.AnalysisReport {
	t.Helper()
	r := runUnlog(t, t.TempDir(), nil, append([]string{"--format", "json"}, args...)...)
	require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)
	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &report), "stdout: %s", r.stdout)
	return report
}

// writeFile writes content to name inside dir and returns the path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestE2EIncidentDBConnection(t *testing.T) {
	report := runJSON(t, "testdata/incidents/db_connection/")

	assert.Contains(t, report.CompactedSummary, "app.log")
	assert.Contains(t, report.CompactedSummary, "connection pool exhausted, shutting down")
	assert.Contains(t, report.CompactedSummary, "circuit breaker open")
	// db.log holds the root cause; it must reach the summary.
	assert.Contains(t, report.CompactedSummary, "db_connection/db.log")
	assert.Contains(t, report.CompactedSummary, "max_connections limit reached")
}

func TestE2EDebugLevelReachesSummary(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`{"level":"debug","msg":"retry budget at 3 of 5","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"payment declined","time":"2024-01-15T10:00:01Z"}
`)

	report := runJSON(t, "--level", "debug", logPath)

	assert.Contains(t, report.CompactedSummary, "retry budget at 3 of 5")
}

func TestE2EIncidentDeployFailure(t *testing.T) {
	report := runJSON(t, "testdata/incidents/deploy_failure/")

	assert.Contains(t, report.CompactedSummary, "deploy_failure/app.log")
	assert.Contains(t, report.CompactedSummary, "deploy_failure/deploy.log")
	assert.Contains(t, report.CompactedSummary, "deployment failure threshold exceeded: initiating rollback")
}

// levelFixture has one entry per level, all within one minute.
const levelFixture = `{"level":"fatal","msg":"disk controller lost","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"write to journal failed","time":"2024-01-15T10:00:01Z"}
{"level":"warn","msg":"queue depth rising","time":"2024-01-15T10:00:02Z"}
{"level":"info","msg":"request served","time":"2024-01-15T10:00:03Z"}
`

func TestE2ELevelPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		configFile string // ~/.unlog/config.toml content; empty means no file
		env        []string
		args       []string
		wantEvents string
	}{
		{
			name:       "default level is warn",
			wantEvents: "Events: 1 fatal, 1 error, 1 warn",
		},
		{
			name:       "config file overrides the default",
			configFile: `level = "error"`,
			wantEvents: "Events: 1 fatal, 1 error, 0 warn",
		},
		{
			name:       "env var overrides the config file",
			configFile: `level = "error"`,
			env:        []string{"UNLOG_LEVEL=fatal"},
			wantEvents: "Events: 1 fatal, 0 error, 0 warn",
		},
		{
			name:       "flag overrides the env var and the config file",
			configFile: `level = "error"`,
			env:        []string{"UNLOG_LEVEL=fatal"},
			args:       []string{"--level", "warn"},
			wantEvents: "Events: 1 fatal, 1 error, 1 warn",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.configFile != "" {
				writeFile(t, home, ".unlog/config.toml", tt.configFile)
			}
			logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)

			args := append([]string{"--format", "json"}, tt.args...)
			r := runUnlog(t, home, tt.env, append(args, logPath)...)
			require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)

			var report types.AnalysisReport
			require.NoError(t, json.Unmarshal([]byte(r.stdout), &report))
			assert.Contains(t, report.CompactedSummary, tt.wantEvents)
		})
	}
}

func TestE2EFormatPrecedence(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)
	home := t.TempDir()
	writeFile(t, home, ".unlog/config.toml", `format = "json"`)

	fromConfig := runUnlog(t, home, nil, logPath)
	require.Equal(t, 0, fromConfig.exit, "stderr: %s", fromConfig.stderr)
	assert.True(t, json.Valid([]byte(fromConfig.stdout)), "config format=json should print JSON, got: %s", fromConfig.stdout)

	fromFlag := runUnlog(t, home, nil, "--format", "markdown", logPath)
	require.Equal(t, 0, fromFlag.exit, "stderr: %s", fromFlag.stderr)
	assert.Contains(t, fromFlag.stdout, "# Incident Analysis")
}

func TestE2EInvalidFlagValues(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"unknown level", []string{"--level", "loud"}, `invalid log level: "loud"`},
		{"unknown format", []string{"--format", "yaml"}, `unsupported output format: "yaml"`},
		{"unparseable since", []string{"--since", "yesterday"}, "invalid --since value"},
		{"unparseable until", []string{"--until", "tomorrow"}, "invalid --until value"},
		{"negative ai timeout", []string{"--ai-timeout", "-1s"}, "--ai-timeout must be positive"},
		{"unparseable ai timeout", []string{"--ai-timeout", "soon"}, "invalid --ai-timeout value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runUnlog(t, t.TempDir(), nil, append(tt.args, logPath)...)
			assert.Equal(t, 1, r.exit)
			assert.Contains(t, r.stderr, tt.wantStderr)
		})
	}
}

func TestE2ETimestampedTextWithPairs(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`2024-01-15 10:00:00 INFO request served path=/a status=200 dur=5ms
2024-01-15 10:00:01 ERROR payment failed user=42 amount=10 currency=USD
2024-01-15 10:00:02 INFO request served path=/b status=200 dur=7ms
`)

	report := runJSON(t, logPath)

	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	assert.Contains(t, critical, "payment failed")
	assert.Contains(t, critical, "2024-01-15T10:00:01Z")
}

// sectionOf returns the body of the summary section that starts at header.
func sectionOf(summary, header string) string {
	start := strings.Index(summary, header)
	if start < 0 {
		return ""
	}
	body := summary[start+len(header):]
	if end := strings.Index(body, "\n## "); end >= 0 {
		body = body[:end]
	}
	return body
}

func TestE2EJSONErrorKeepsTraceback(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "worker.log",
		`{"level":"info","msg":"starting","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"unhandled exception in worker","time":"2024-01-15T10:00:01Z"}
Traceback (most recent call last):
  File "worker.py", line 10, in run
ValueError: bad input
{"level":"info","msg":"recovered","time":"2024-01-15T10:00:02Z"}
`)

	report := runJSON(t, logPath)

	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	assert.Contains(t, critical, "unhandled exception in worker")
	assert.Contains(t, critical, "ValueError: bad input")
}

func TestE2EVerboseReportsUnparsedLines(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`starting app v2 (build 1f3a)
{"level":"error","msg":"bind failed","time":"2024-01-15T10:00:01Z"}
{"level":"info","msg":"retrying","time":"2024-01-15T10:00:02Z"}
`)

	r := runUnlog(t, t.TempDir(), nil, "-v", logPath)

	require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)
	assert.Contains(t, r.stderr, "format=json entries=3")
	assert.Contains(t, r.stderr, "unparsed=1")
}

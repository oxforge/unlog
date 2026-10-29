package main

// End-to-end tests: they build the real binary once and run it the way a user
// does, with the CLI's own defaults, an isolated HOME (so ~/.unlog/config.toml
// is never the developer's) and only the environment variables a test sets.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oxforge/unlog/types"
)

var unlogBin string

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

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
	// db.log holds the root cause; it must reach the summary as errors.
	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	assert.Contains(t, critical, "2024-01-15T10:31:10Z [FATAL] testdata/incidents/db_connection/db.log[postgres]: FATAL:  out of memory: shared buffers exhausted")
	assert.Contains(t, report.CompactedSummary, "[ERROR] testdata/incidents/db_connection/db.log[postgres]: ERROR:  max_connections limit reached")
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
	// The deploy lines are INFO, yet they reach the summary ahead of the
	// errors, and the deployment-failure chain forms across both files.
	assert.Contains(t, sectionOf(report.CompactedSummary, "## Deployments"), "deployment started: app-deployment to version 3.3.0")
	assert.Contains(t, report.CompactedSummary, "Last deployment: 2024-01-15T14:02:00Z, 15s before the first error")
	assert.Contains(t, sectionOf(report.CompactedSummary, "## Error Chains"), "deployment-failure: 3 of 3 steps")
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

func TestE2EOversizedLineDoesNotStopTheFile(t *testing.T) {
	long := `{"level":"error","msg":"dumped payload","payload":"` + strings.Repeat("x", 1_100_000) + `","time":"2024-01-15T10:00:01Z"}`
	logPath := writeFile(t, t.TempDir(), "app.log",
		`{"level":"error","msg":"first failure","time":"2024-01-15T10:00:00Z"}
`+long+`
{"level":"error","msg":"failure after the big line","time":"2024-01-15T10:00:02Z"}
`)

	report := runJSON(t, logPath)

	assert.Contains(t, report.CompactedSummary, "failure after the big line")
	assert.Less(t, len(report.CompactedSummary), 64*1024)
}

func TestE2ENoisePatternsDoNotDropErrors(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`{"level":"error","msg":"max pool size reached, rejecting connections","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"failed to push /metrics to gateway: 500","time":"2024-01-15T10:00:01Z"}
{"level":"error","msg":"retrying request failed permanently after 5 attempts","time":"2024-01-15T10:00:02Z"}
`)

	report := runJSON(t, logPath)

	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	assert.Contains(t, critical, "max pool size reached")
	assert.Contains(t, critical, "failed to push /metrics to gateway")
	assert.Contains(t, critical, "retrying request failed permanently")
}

func TestE2EDockerStderrIsNotAnError(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "container.log",
		`{"log":"2024/01/15 10:00:00 server listening on :8080\n","stream":"stderr","time":"2024-01-15T10:00:00Z"}
{"log":"{\"level\":\"info\",\"msg\":\"cache warmed\"}\n","stream":"stderr","time":"2024-01-15T10:00:01Z"}
{"log":"{\"level\":\"error\",\"msg\":\"payment provider declined the charge\",\"provider\":\"acme\"}\n","stream":"stderr","time":"2024-01-15T10:00:02Z"}
`)

	report := runJSON(t, logPath)

	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	assert.Contains(t, critical, "payment provider declined the charge")
	assert.NotContains(t, critical, "server listening")
	assert.NotContains(t, critical, "cache warmed")
}

// The enterprise app validates the report with a Zod schema
// (unlog-enterprise/packages/shared/src/report-schema.ts) that requires these
// keys even when there is no AI analysis and no error.
func TestE2EReportHasEveryKeyTheEnterpriseSchemaRequires(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`{"level":"warn","msg":"queue depth rising","time":"2024-01-15T10:00:00Z"}
{"level":"warn","msg":"retry budget low","time":"2024-01-15T10:00:01Z"}
`)
	r := runUnlog(t, t.TempDir(), nil, "--format", "json", logPath)
	require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &report))
	for _, key := range []string{"generated_at", "unlog_version", "model_used", "analysis_duration_ms",
		"ai_duration_ms", "stats", "analysis", "compacted_summary"} {
		assert.Contains(t, report, key)
	}
	stats, ok := report["stats"].(map[string]any)
	require.True(t, ok, "stats should be an object")
	for _, key := range []string{"ingested", "dropped", "survived", "unique_signatures", "time_window_start",
		"time_window_end", "auto_detected_window", "source_breakdown", "file_count", "bytes_processed", "duration_ms"} {
		assert.Contains(t, stats, key)
	}
	_, isObject := stats["source_breakdown"].(map[string]any)
	assert.True(t, isObject, "source_breakdown should be an object, got %v", stats["source_breakdown"])
	assert.Equal(t, "", report["model_used"])
	assert.Equal(t, "", report["analysis"])
	assert.Equal(t, 0.0, report["ai_duration_ms"])
}

func TestE2ETwoIncidentsHoursApartAreBothReported(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`{"level":"error","msg":"payment gateway refused card","time":"2024-01-15T10:00:01Z"}
{"level":"error","msg":"payment gateway refused card again","time":"2024-01-15T10:00:02Z"}
{"level":"error","msg":"order rollback","time":"2024-01-15T10:00:03Z"}
{"level":"error","msg":"disk quota exceeded on /data","time":"2024-01-15T14:00:01Z"}
{"level":"error","msg":"write failed for batch","time":"2024-01-15T14:00:02Z"}
{"level":"error","msg":"batch job aborted","time":"2024-01-15T14:00:03Z"}
`)

	first := runJSON(t, logPath).CompactedSummary
	assert.Contains(t, first, "order rollback")
	assert.Contains(t, first, "batch job aborted")
	for i := 0; i < 9; i++ {
		assert.Equal(t, first, runJSON(t, logPath).CompactedSummary, "run %d differs", i+2)
	}
}

// repeatedErrors returns n JSON error lines of one signature, one per second
// from start: "payment declined for order 0", "... order 1", ...
func repeatedErrors(n int, start time.Time) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `{"level":"error","msg":"payment declined for order %d","time":"%s"}`+"\n",
			i, start.Add(time.Duration(i)*time.Second).Format(time.RFC3339))
	}
	return b.String()
}

func TestE2ERepeatedErrorIsOneLine(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		repeatedErrors(3000, time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)))

	report := runJSON(t, logPath)

	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	var lines []string
	for _, line := range strings.Split(critical, "\n") {
		if strings.Contains(line, "payment declined") {
			lines = append(lines, line)
		}
	}
	require.Len(t, lines, 1, "critical section:\n%s", critical)
	assert.True(t, strings.HasPrefix(lines[0], "2024-01-15T10:00:00Z–10:49:59Z [ERROR]"), "got %q", lines[0])
	assert.Contains(t, lines[0], "payment declined for order 0")
	assert.Contains(t, lines[0], "{×3000")
}

func TestE2ESameInputSameSummary(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	first := writeFile(t, dir, "a.log", repeatedErrors(3000, start))
	second := writeFile(t, dir, "b.log", repeatedErrors(3000, start.Add(30*time.Minute)))

	want := runJSON(t, first, second).CompactedSummary
	for i := 0; i < 19; i++ {
		assert.Equal(t, want, runJSON(t, first, second).CompactedSummary, "run %d differs", i+2)
	}
}

func TestE2EHighestSeverityWins(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log",
		`{"level":"warn","msg":"disk 91% full on /data","time":"2024-01-15T10:00:00Z"}
{"level":"error","msg":"disk 97% full on /data","time":"2024-01-15T10:05:00Z"}
`)

	summary := runJSON(t, logPath).CompactedSummary

	assert.Equal(t, 1, strings.Count(summary, "full on /data"), "summary:\n%s", summary)
	critical := sectionOf(summary, "## Critical Errors")
	assert.Contains(t, critical, "2024-01-15T10:00:00Z–10:05:00Z [ERROR]")
	assert.Contains(t, critical, "disk 91% full on /data")
	// Each occurrence counts at its own level in the overview.
	assert.Contains(t, summary, "Events: 0 fatal, 1 error, 1 warn\n")
}

func TestE2EOneLinePerSource(t *testing.T) {
	dir := t.TempDir()
	line := `{"level":"error","msg":"payment declined for order 5","time":"2024-01-15T10:00:00Z"}` + "\n"
	api1 := writeFile(t, dir, "api-1.log", line)
	api2 := writeFile(t, dir, "api-2.log", line)

	summary := runJSON(t, api1, api2).CompactedSummary

	assert.Equal(t, 2, strings.Count(summary, "payment declined for order 5"), "summary:\n%s", summary)
	assert.Contains(t, summary, "api-1.log: payment declined")
	assert.Contains(t, summary, "api-2.log: payment declined")
}

func TestE2EVerboseStatsLabels(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", strings.Repeat(
		`{"level":"error","msg":"disk full","time":"2024-01-15T10:00:00Z"}`+"\n", 30))

	r := runUnlog(t, t.TempDir(), nil, "-v", logPath)

	require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)
	assert.Regexp(t, `Merged duplicates:\s+29`, r.stderr)
	assert.Contains(t, r.stderr, "Bursts:")
	assert.NotContains(t, r.stderr, "Spike events")
	assert.NotContains(t, r.stderr, "auto-window")
}

// burstLines returns spec example 4's input: a background error every 10 s
// for 10 minutes from 10:00:00 and, when withBackground is false, only the 400
// payment errors at 25 ms steps from 10:05:00.
func burstLines(withBackground bool) string {
	var b strings.Builder
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	type line struct {
		at  time.Time
		msg string
	}
	var lines []line
	if withBackground {
		for i := 0; i < 60; i++ {
			lines = append(lines, line{base.Add(time.Duration(i) * 10 * time.Second), fmt.Sprintf("cache miss for key user:%d", i)})
		}
	}
	for i := 0; i < 400; i++ {
		lines = append(lines, line{base.Add(5*time.Minute + time.Duration(i)*25*time.Millisecond),
			fmt.Sprintf("payment gateway refused connection from 10.0.3.%d", i%250)})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].at.Before(lines[j].at) })
	for _, l := range lines {
		fmt.Fprintf(&b, `{"level":"error","msg":%q,"time":%q}`+"\n", l.msg, l.at.Format(time.RFC3339Nano))
	}
	return b.String()
}

func TestE2EBurstIsReported(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "same.log", burstLines(true))

	anomalies := sectionOf(runJSON(t, logPath).CompactedSummary, "## Rate Anomalies")

	assert.Contains(t, anomalies, "2024-01-15T10:05:00Z–10:05:09Z")
	assert.Contains(t, anomalies, "401 events in 10s (normally quiet)")
	assert.Contains(t, anomalies, "payment gateway refused connection")
}

func TestE2EExcerptHasNoBurst(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "same.log", burstLines(false))

	summary := runJSON(t, logPath).CompactedSummary

	assert.Equal(t, "\n(none)\n", sectionOf(summary, "## Rate Anomalies"))
	critical := sectionOf(summary, "## Critical Errors")
	assert.Contains(t, critical, "2024-01-15T10:05:00Z–10:05:09Z [ERROR]")
	assert.Contains(t, critical, "{×400}")
}

// TestE2EGoldenSummaries pins whole summaries, so any change to what reaches
// the reader shows up as a reviewable diff. After an intended change, run
// `just golden` and review the diff of testdata/golden/. Fixtures with
// year-less syslog lines are left out: their year comes from the file's mtime,
// which differs per checkout.
func TestE2EGoldenSummaries(t *testing.T) {
	tests := []struct {
		name    string
		sources []string
	}{
		{"db_connection", []string{"testdata/incidents/db_connection/"}},
		{"deploy_failure", []string{"testdata/incidents/deploy_failure/"}},
		{"formats", []string{
			"testdata/formats/apache_clf.log", "testdata/formats/cloudwatch.log",
			"testdata/formats/csv_kibana.csv", "testdata/formats/docker_compose.log",
			"testdata/formats/docker_mixed_streams.log", "testdata/formats/generic_with_brackets.log",
			"testdata/formats/json_epoch.json", "testdata/formats/json_multiline_stack.log",
			"testdata/formats/k8s_deployment.log", "testdata/formats/kubernetes_multiline.log",
			"testdata/formats/logfmt_varied.log", "testdata/formats/syslog_rfc5424.log",
			"testdata/formats/syslog_rsyslog_iso.log", "testdata/formats/unstructured.log",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runJSON(t, tt.sources...).CompactedSummary + "\n"
			golden := filepath.Join("testdata", "golden", tt.name+".txt")
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
				return
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err, "missing golden file; run `just golden`")
			assert.Equal(t, string(want), got)
		})
	}
}

func TestE2EMissingInputFails(t *testing.T) {
	existing := writeFile(t, t.TempDir(), "app.log", levelFixture)
	empty := t.TempDir()
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"missing file", []string{"nope.log"}, "nope.log"},
		{"one of several files missing", []string{existing, "nope.log"}, "nope.log"},
		{"directory without log files", []string{empty}, "no log files found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runUnlog(t, t.TempDir(), nil, append([]string{"--format", "json"}, tt.args...)...)
			assert.Equal(t, 1, r.exit, "stdout: %s", r.stdout)
			assert.Contains(t, r.stderr, tt.wantStderr)
		})
	}
}

func TestE2EAnalyzeCommandIsTheDefault(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)

	r := runUnlog(t, t.TempDir(), nil, "analyze", "--format", "json", "--level", "error", logPath)

	require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)
	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &report))
	assert.Contains(t, report.CompactedSummary, "Events: 1 fatal, 1 error, 0 warn")
}

func TestE2EStdinAlongsideFiles(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)
	cmd := exec.Command(unlogBin, "--format", "json", "-", logPath)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	cmd.Stdin = strings.NewReader(`{"level":"error","msg":"piped failure","time":"2024-01-15T10:00:05Z"}` + "\n")
	out, err := cmd.Output()
	require.NoError(t, err)

	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal(out, &report))
	assert.Contains(t, report.CompactedSummary, "stdin: piped failure")
	assert.Contains(t, report.CompactedSummary, "app.log")
}

const zonelessFixture = `2024-01-15 10:00:00 ERROR early failure
2024-01-15 11:00:00 ERROR late failure
`

// The same zone-less string means the same instant in a flag and in a log
// line, whatever the machine's time zone.
func TestE2ESinceUsesTheLogsZone(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", zonelessFixture)

	r := runUnlog(t, t.TempDir(), []string{"TZ=Asia/Tokyo"}, "--format", "json", "--since", "2024-01-15 10:30:00", logPath)

	require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)
	var report types.AnalysisReport
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &report))
	assert.Contains(t, report.CompactedSummary, "late failure")
	assert.NotContains(t, report.CompactedSummary, "early failure")
}

func TestE2ETZFlagSetsTheZoneOfZonelessTimestamps(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", zonelessFixture)

	report := runJSON(t, "--tz", "Asia/Tokyo", "--since", "2024-01-15 10:30:00", logPath)

	assert.Contains(t, report.CompactedSummary, "2024-01-15T02:00:00Z [ERROR]")
	assert.NotContains(t, report.CompactedSummary, "early failure")
}

func TestE2ETZFromTheEnvironmentAndConfig(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", zonelessFixture)
	withConfig := t.TempDir()
	writeFile(t, filepath.Join(withConfig, ".unlog"), "config.toml", "tz = \"Asia/Tokyo\"\n")

	for name, run := range map[string]cliRun{
		"UNLOG_TZ": runUnlog(t, t.TempDir(), []string{"UNLOG_TZ=Asia/Tokyo"}, "--format", "json", logPath),
		"config":   runUnlog(t, withConfig, nil, "--format", "json", logPath),
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, 0, run.exit, "stderr: %s", run.stderr)
			var report types.AnalysisReport
			require.NoError(t, json.Unmarshal([]byte(run.stdout), &report))
			assert.Contains(t, report.CompactedSummary, "2024-01-15T02:00:00Z [ERROR]")
		})
	}
}

func TestE2EInvalidTZNamesWhereItCameFrom(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", zonelessFixture)
	withConfig := t.TempDir()
	writeFile(t, filepath.Join(withConfig, ".unlog"), "config.toml", "tz = \"Mars/Olympus\"\n")

	tests := []struct {
		name string
		run  cliRun
		want string
	}{
		{"flag", runUnlog(t, t.TempDir(), nil, "--tz", "Mars/Olympus", logPath), "invalid --tz value"},
		{"environment", runUnlog(t, t.TempDir(), []string{"UNLOG_TZ=Mars/Olympus"}, logPath), "invalid UNLOG_TZ value"},
		{"config", runUnlog(t, withConfig, nil, logPath), "invalid tz value in the config file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, 1, tt.run.exit)
			assert.Contains(t, tt.run.stderr, tt.want)
		})
	}
}

// A misconfigured provider fails before any log is read: the input path
// below does not exist, and the error is about the provider, not the file.
func TestE2EProviderIsCheckedBeforeReadingInput(t *testing.T) {
	tests := []struct {
		name       string
		env        []string
		provider   string
		wantStderr string
	}{
		{"unknown provider", nil, "antropic", `unknown AI provider: "antropic"`},
		{"missing API key", nil, "anthropic", "ANTHROPIC_API_KEY not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runUnlog(t, t.TempDir(), tt.env, "--ai-provider", tt.provider, "nope.log")
			assert.Equal(t, 1, r.exit)
			assert.Contains(t, r.stderr, tt.wantStderr)
			assert.NotContains(t, r.stderr, "nope.log")
		})
	}
}

// An AI failure still delivers the report, and exits 3 so a caller can tell
// it from a run with no report (1). The proxy points at a closed local port,
// so the call fails at once without touching the network.
func TestE2EFailedAnalysisStillWritesTheReport(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)
	env := []string{"HTTPS_PROXY=http://127.0.0.1:1", "ANTHROPIC_API_KEY=test-key"}
	args := []string{"--ai-provider", "anthropic", "--ai-timeout", "5s"}

	t.Run("json", func(t *testing.T) {
		r := runUnlog(t, t.TempDir(), env, append(args, "--format", "json", logPath)...)

		assert.Equal(t, 3, r.exit)
		var report map[string]any
		require.NoError(t, json.Unmarshal([]byte(r.stdout), &report), "stdout: %s", r.stdout)
		assert.Contains(t, report["compacted_summary"], "disk controller lost")
		assert.Contains(t, report["analysis_error"], "proxyconnect")
		assert.Equal(t, 1, strings.Count(r.stderr, "proxyconnect"), "the error is printed once: %s", r.stderr)
	})

	t.Run("text to a file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "report.txt")
		r := runUnlog(t, t.TempDir(), env, append(args, "--output", out, logPath)...)

		assert.Equal(t, 3, r.exit)
		written, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Contains(t, string(written), "disk controller lost")
		assert.Contains(t, string(written), "AI analysis failed:")
	})
}

// Errors print once, without the usage text, and without each layer naming
// the one below twice ("pipeline: pipeline: ingest: ingest: ...").
func TestE2EErrorsPrintOnceWithoutUsage(t *testing.T) {
	r := runUnlog(t, t.TempDir(), nil, filepath.Join(t.TempDir(), "nope.log"))

	assert.Equal(t, 1, r.exit)
	assert.Equal(t, 1, strings.Count(r.stderr, "nope.log"), "stderr: %s", r.stderr)
	assert.NotContains(t, r.stderr, "Usage:")
	assert.NotContains(t, r.stderr, "pipeline: pipeline")
	assert.NotContains(t, r.stderr, "ingest: ingest")

	flag := runUnlog(t, t.TempDir(), nil, "--colour", "x.log")
	assert.Equal(t, 1, flag.exit)
	assert.Contains(t, flag.stderr, "unknown flag: --colour")
	assert.Contains(t, flag.stderr, "unlog --help")
	assert.NotContains(t, flag.stderr, "Usage:")
}

// --output may be a device or a pipe, not only a regular file.
func TestE2EOutputToADevice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev on Windows")
	}
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)

	null := runUnlog(t, t.TempDir(), nil, "--output", "/dev/null", logPath)
	assert.Equal(t, 0, null.exit, "stderr: %s", null.stderr)

	stdout := runUnlog(t, t.TempDir(), nil, "--output", "/dev/stdout", logPath)
	assert.Equal(t, 0, stdout.exit, "stderr: %s", stdout.stderr)
	assert.Contains(t, stdout.stdout, "disk controller lost")
}

func TestE2EBadConfigErrorNamesEachLayerOnce(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".unlog"), "config.toml", "tz = [\n")

	r := runUnlog(t, home, nil, writeFile(t, t.TempDir(), "app.log", levelFixture))

	assert.Equal(t, 1, r.exit)
	assert.Contains(t, r.stderr, "config")
	assert.NotContains(t, r.stderr, "config: config")
}

// --output is checked before any input is read or any AI call is paid for,
// and a failed run leaves an existing output file as it was.
func TestE2EOutputFileIsCheckedFirstAndKeptOnFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.log")

	r := runUnlog(t, t.TempDir(), nil, "--output", filepath.Join(t.TempDir(), "no", "such", "dir", "r.txt"), missing)
	assert.Equal(t, 1, r.exit)
	assert.Contains(t, r.stderr, "output file")
	assert.NotContains(t, r.stderr, "nope.log")

	dir := t.TempDir()
	previous := writeFile(t, dir, "previous.txt", "last week's report\n")
	fresh := filepath.Join(dir, "fresh.txt")
	for _, out := range []string{previous, fresh} {
		r := runUnlog(t, t.TempDir(), nil, "--output", out, missing)
		assert.Equal(t, 1, r.exit)
	}
	kept, err := os.ReadFile(previous)
	require.NoError(t, err)
	assert.Equal(t, "last week's report\n", string(kept))
	assert.NoFileExists(t, fresh)
}

func TestE2EReportHasAnEmptyAnalysisErrorWithoutAI(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "app.log", levelFixture)
	r := runUnlog(t, t.TempDir(), nil, "--format", "json", logPath)
	require.Equal(t, 0, r.exit)

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &report))
	assert.Equal(t, "", report["analysis_error"])
}

func TestE2EServicesInOneFileAreToldApart(t *testing.T) {
	logPath := writeFile(t, t.TempDir(), "agg.log",
		`{"level":"error","service":"payments","msg":"upstream refused","time":"2024-01-15T10:00:00Z"}
{"level":"error","service":"search","msg":"upstream refused","time":"2024-01-15T10:00:02Z"}
`)

	report := runJSON(t, logPath)

	critical := sectionOf(report.CompactedSummary, "## Critical Errors")
	assert.Contains(t, critical, "agg.log[payments]: upstream refused")
	assert.Contains(t, critical, "agg.log[search]: upstream refused {with="+logPath+"[payments]}")
	assert.Equal(t, map[string]int64{logPath: 2}, report.Stats.SourceBreakdown, "source_breakdown stays per file")
}

// The report file is never read as input: not when it sits in a searched
// directory, and naming it as an input is an error that leaves it alone.
func TestE2EOutputFileIsNotAnInput(t *testing.T) {
	dir := t.TempDir()
	logPath := writeFile(t, dir, "app.log", levelFixture)
	out := filepath.Join(dir, "report.txt")

	for range 2 {
		r := runUnlog(t, t.TempDir(), nil, "--output", out, dir)
		require.Equal(t, 0, r.exit, "stderr: %s", r.stderr)
	}
	written, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(written), "Events: 1 fatal, 1 error, 1 warn", "the earlier report isn't counted")

	r := runUnlog(t, t.TempDir(), nil, "--output", logPath, logPath)
	assert.Equal(t, 1, r.exit)
	assert.Contains(t, r.stderr, "--output")
	kept, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, levelFixture, string(kept))
}

// -v shows why files were skipped.
func TestE2EVerboseNamesSkippedFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.log", levelFixture)
	writeFile(t, dir, "wtmp", "\x00\x00\x07\x00root\x00")

	r := runUnlog(t, t.TempDir(), nil, "-v", dir)

	assert.Equal(t, 0, r.exit)
	assert.Contains(t, r.stderr, "wtmp")
}

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/oxforge/unlog/filter"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runPipeline runs the full pipeline against sources with default filter opts
// (min level = warn) and returns the result.
func runPipeline(t *testing.T, sources []string) *pipeline.Result {
	t.Helper()
	opts := filter.DefaultFilterOptions()
	opts.MinLevel = 4 // Warn
	result, err := pipeline.New(pipeline.Options{FilterOpts: opts}).Run(context.Background(), sources)
	require.NoError(t, err)
	return result
}

// renderStats calls printStats with showDetailed=true and returns the output.
func renderStats(result *pipeline.Result) string {
	var buf bytes.Buffer
	printStats(&buf, result, nil, true)
	return buf.String()
}

// TestStatsOutput runs the full pipeline against each testdata file and verifies
// the filter stats and verbose source output contain expected values.
func TestStatsOutput(t *testing.T) {
	tests := []struct {
		name string
		file string

		// Expected filter stats.
		ingested int64

		// Expected source line fragments in verbose output.
		sourceFormat  string
		sourceEntries int64
		// Level fragments expected in the source line (e.g. "ERROR=3").
		sourceLevels []string
		// Level fragments that must NOT appear.
		sourceLevelsAbsent []string
	}{
		{
			name:          "json structured",
			file:          "../testdata/formats/json_structured.log",
			ingested:      7,
			sourceFormat:  "format=json",
			sourceEntries: 7,
			sourceLevels:  []string{"FATAL=1", "ERROR=3", "WARN=1", "INFO=2"},
		},
		{
			name:          "json severity field",
			file:          "../testdata/formats/json_severity.json",
			ingested:      7,
			sourceFormat:  "format=json",
			sourceEntries: 7,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=2", "DEBUG=1"},
		},
		{
			name:          "json epoch timestamps",
			file:          "../testdata/formats/json_epoch.json",
			ingested:      6,
			sourceFormat:  "format=json",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=2"},
		},
		{
			name:          "json multiline stacktrace",
			file:          "../testdata/formats/json_multiline_stack.log",
			ingested:      6,
			sourceFormat:  "format=json",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1"},
		},
		{
			name:          "csv with levels",
			file:          "../testdata/formats/csv_with_levels.csv",
			ingested:      8,
			sourceFormat:  "format=csv",
			sourceEntries: 8,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=3"},
		},
		{
			name:          "csv structured (.log)",
			file:          "../testdata/formats/csv_structured.log",
			ingested:      6,
			sourceFormat:  "format=csv",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=2"},
		},
		{
			name:          "csv inferred levels",
			file:          "../testdata/formats/csv_inferred_levels.csv",
			ingested:      10,
			sourceFormat:  "format=csv",
			sourceEntries: 10,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=2", "unknown=2"},
		},
		{
			name:          "csv kibana export",
			file:          "../testdata/formats/csv_kibana.csv",
			ingested:      20,
			sourceFormat:  "format=csv",
			sourceEntries: 20,
			sourceLevels:  []string{"FATAL=1", "ERROR=4", "WARN=2", "INFO=3", "DEBUG=1", "unknown=9"},
		},
		{
			name:          "docker json",
			file:          "../testdata/formats/docker_json.log",
			ingested:      7,
			sourceFormat:  "format=docker-json",
			sourceEntries: 7,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=3"},
		},
		{
			name:          "docker mixed streams",
			file:          "../testdata/formats/docker_mixed_streams.log",
			ingested:      9,
			sourceFormat:  "format=docker-json",
			sourceEntries: 9,
			sourceLevels:  []string{"ERROR=6", "INFO=3"},
		},
		{
			name:          "kubernetes",
			file:          "../testdata/formats/kubernetes.log",
			ingested:      6,
			sourceFormat:  "format=kubernetes",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=1", "ERROR=1", "WARN=1", "INFO=3"},
		},
		{
			name:          "kubernetes multiline",
			file:          "../testdata/formats/kubernetes_multiline.log",
			ingested:      10,
			sourceFormat:  "format=kubernetes",
			sourceEntries: 10,
			sourceLevels:  []string{"FATAL=1", "ERROR=6", "WARN=1", "INFO=2"},
		},
		{
			name:          "logfmt",
			file:          "../testdata/formats/logfmt.log",
			ingested:      6,
			sourceFormat:  "format=logfmt",
			sourceEntries: 6,
			sourceLevels:  []string{"ERROR=3", "WARN=1", "INFO=2"},
		},
		{
			name:          "logfmt varied",
			file:          "../testdata/formats/logfmt_varied.log",
			ingested:      7,
			sourceFormat:  "format=logfmt",
			sourceEntries: 7,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=2", "DEBUG=1"},
		},
		{
			name:          "syslog rfc3164",
			file:          "../testdata/formats/syslog.log",
			ingested:      6,
			sourceFormat:  "format=syslog-rfc3164",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=2"},
		},
		{
			name:          "syslog rfc5424",
			file:          "../testdata/formats/syslog_rfc5424.log",
			ingested:      6,
			sourceFormat:  "format=syslog-rfc5424",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=2", "ERROR=1", "WARN=3"},
		},
		{
			name:               "apache clf",
			file:               "../testdata/formats/apache_clf.log",
			ingested:           6,
			sourceFormat:       "format=clf",
			sourceEntries:      6,
			sourceLevels:       []string{"ERROR=3", "WARN=1", "INFO=2"},
			sourceLevelsAbsent: []string{"unknown="},
		},
		{
			name:          "cloudwatch",
			file:          "../testdata/formats/cloudwatch.log",
			ingested:      6,
			sourceFormat:  "format=cloudwatch",
			sourceEntries: 6,
			sourceLevels:  []string{"ERROR=2", "WARN=1", "unknown=3"},
		},
		{
			name:          "generic timestamps",
			file:          "../testdata/formats/generic_timestamp.log",
			ingested:      6,
			sourceFormat:  "format=generic",
			sourceEntries: 6,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=2"},
		},
		{
			name:          "generic with brackets",
			file:          "../testdata/formats/generic_with_brackets.log",
			ingested:      10,
			sourceFormat:  "format=generic",
			sourceEntries: 10,
			sourceLevels:  []string{"FATAL=1", "ERROR=4", "WARN=2", "INFO=2", "DEBUG=1"},
		},
		{
			name:          "unstructured",
			file:          "../testdata/formats/unstructured.log",
			ingested:      8,
			sourceFormat:  "format=raw",
			sourceEntries: 8,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "unknown=4"},
		},
		{
			name:          "raw with levels",
			file:          "../testdata/formats/raw_with_levels.log",
			ingested:      9,
			sourceFormat:  "format=raw",
			sourceEntries: 9,
			sourceLevels:  []string{"FATAL=1", "ERROR=2", "WARN=1", "INFO=1", "unknown=4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runPipeline(t, []string{tt.file})
			output := renderStats(result)

			// --- Filter Stats section ---
			assert.Contains(t, output, "--- Filter Stats ---")
			assert.Contains(t, output,
				fmt.Sprintf("Ingested:           %d", tt.ingested))

			// Dropped + survived = ingested.
			assert.Equal(t, tt.ingested,
				result.Stats.TotalDropped+result.Stats.TotalSurvived,
				"dropped + survived should equal ingested")

			// --- Detailed Breakdown section ---
			assert.Contains(t, output, "--- Detailed Breakdown ---")
			assert.Contains(t, output, "Dropped by level:")
			assert.Contains(t, output, "Dropped by dedup:")

			// --- Sources section ---
			assert.Contains(t, output, "--- Sources ---")

			// Find the source line for this file.
			var sourceLine string
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, tt.file) {
					sourceLine = line
					break
				}
			}
			require.NotEmpty(t, sourceLine,
				"source line for %s not found in output:\n%s", tt.file, output)

			assert.Contains(t, sourceLine, tt.sourceFormat)
			assert.Contains(t, sourceLine,
				fmt.Sprintf("entries=%d", tt.sourceEntries))

			for _, frag := range tt.sourceLevels {
				assert.Contains(t, sourceLine, frag,
					"source line should contain %s", frag)
			}
			for _, frag := range tt.sourceLevelsAbsent {
				assert.NotContains(t, sourceLine, frag,
					"source line should NOT contain %s", frag)
			}
		})
	}
}

// TestStatsOutputMultiFile verifies that running multiple files produces
// correct per-source stats with all sources listed.
func TestStatsOutputMultiFile(t *testing.T) {
	files := []string{
		"../testdata/formats/json_structured.log",
		"../testdata/formats/csv_with_levels.csv",
		"../testdata/formats/logfmt.log",
	}

	result := runPipeline(t, files)
	output := renderStats(result)

	// All three sources should appear.
	for _, f := range files {
		assert.Contains(t, output, f)
	}

	// Total ingested = 7 + 8 + 6 = 21.
	assert.Equal(t, int64(21), result.Stats.TotalIngested)

	// Each source should have the right format.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		switch {
		case strings.Contains(line, "json_structured.log"):
			assert.Contains(t, line, "format=json")
			assert.Contains(t, line, "entries=7")
		case strings.Contains(line, "csv_with_levels.csv"):
			assert.Contains(t, line, "format=csv")
			assert.Contains(t, line, "entries=8")
		case strings.Contains(line, "logfmt.log"):
			assert.Contains(t, line, "format=logfmt")
			assert.Contains(t, line, "entries=6")
		}
	}
}

// TestStatsConsistency verifies that the filter stats are internally consistent:
// dropped + survived = ingested, and all detailed drop categories sum correctly.
func TestStatsConsistency(t *testing.T) {
	files := []string{
		"../testdata/formats/json_structured.log",
		"../testdata/formats/docker_json.log",
		"../testdata/formats/csv_with_levels.csv",
		"../testdata/formats/syslog.log",
		"../testdata/formats/kubernetes.log",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			result := runPipeline(t, []string{f})

			fs := result.Stats
			ds := result.DetailedStats

			// Basic identity.
			assert.Equal(t, fs.TotalIngested,
				fs.TotalDropped+fs.TotalSurvived,
				"dropped + survived = ingested")

			// Detailed drops should not exceed total dropped.
			detailedSum := ds.DroppedByLevel + ds.DroppedByTimeWindow +
				ds.DroppedByNoise + ds.DroppedByDedup + ds.DroppedByAutoWindow
			assert.LessOrEqual(t, detailedSum, fs.TotalDropped,
				"detailed drop sum ≤ total dropped")

			// Ingest stats entries should sum to ingested count.
			var ingestTotal int64
			for _, ss := range result.IngestStats {
				ingestTotal += ss.Entries
			}
			assert.Equal(t, fs.TotalIngested, ingestTotal,
				"sum of per-source entries = total ingested")
		})
	}
}

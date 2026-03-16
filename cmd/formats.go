package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/ingest"
)

const formatsSampleLines = 100

var formatsFormatFlag string

var formatsCmd = &cobra.Command{
	Use:   "formats [files...]",
	Short: "Detect and display log formats found in the input files",
	RunE:  runFormats,
}

func init() {
	formatsCmd.Flags().StringVar(&formatsFormatFlag, "format", "", "Output format: text, json (default: text)")
	rootCmd.AddCommand(formatsCmd)
}

// formatResult holds a file path and its detected log format.
type formatResult struct {
	File   string `json:"file"`
	Format string `json:"format"`
}

func runFormats(cmd *cobra.Command, args []string) error {
	effectiveFormat, err := resolveFormat(cmd, formatsFormatFlag)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return fmt.Errorf("cmd: formats requires at least one file or directory")
	}

	files, err := ingest.ExpandSources(args)
	if err != nil {
		return fmt.Errorf("cmd: formats: %w", err)
	}

	results := make([]formatResult, 0, len(files))
	for _, path := range files {
		format, err := detectFileFormat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "formats: skipping %s: %v\n", path, err)
			continue
		}
		results = append(results, formatResult{
			File:   path,
			Format: format,
		})
	}

	switch effectiveFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return fmt.Errorf("cmd: formats json: %w", err)
		}
	default:
		for _, r := range results {
			_, _ = fmt.Fprintf(os.Stdout, "%-30s %s\n", r.File+":", r.Format)
		}
	}

	return nil
}

// detectFileFormat opens a file, reads sample lines, and returns the detected format string.
func detectFileFormat(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	lines := sampleLines(f, formatsSampleLines)
	format := ingest.DetectFormat(lines)
	return format.String(), nil
}

// sampleLines reads up to n non-blank lines from r. Only non-blank lines count
// toward the limit, so blank lines in the input don't reduce the sample size.
func sampleLines(r io.Reader, n int) []string {
	sc := bufio.NewScanner(r)
	var lines []string
	for sc.Scan() && len(lines) < n {
		line := sc.Text()
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

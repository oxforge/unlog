package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/internal/pipeline"
)

var (
	statsLevelFlag     string
	statsSinceFlag     string
	statsUntilFlag     string
	statsNoiseFileFlag string
	statsFormatFlag    string
)

var statsCmd = &cobra.Command{
	Use:   "stats [files...]",
	Short: "Show log statistics and filter results without AI analysis",
	RunE:  runStats,
}

func init() {
	statsCmd.Flags().StringVar(&statsLevelFlag, "level", "", "Minimum log level: trace, debug, info, warn, error, fatal")
	statsCmd.Flags().StringVar(&statsSinceFlag, "since", "", "Start time filter (ISO 8601 or relative: \"2h\", \"30m\")")
	statsCmd.Flags().StringVar(&statsUntilFlag, "until", "", "End time filter (ISO 8601 or relative: \"2h\", \"30m\")")
	statsCmd.Flags().StringVar(&statsNoiseFileFlag, "noise-file", "", "Path to custom noise patterns file")
	statsCmd.Flags().StringVar(&statsFormatFlag, "format", "", "Output format: text, json (default: text)")
	rootCmd.AddCommand(statsCmd)
}

func runStats(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	effectiveFormat, err := resolveFormat(cmd, statsFormatFlag)
	if err != nil {
		return err
	}

	filterOpts, err := buildFilterOpts(cmd, statsLevelFlag, statsSinceFlag, statsUntilFlag, statsNoiseFileFlag, args)
	if err != nil {
		return err
	}

	opts := pipeline.Options{
		FilterOpts: filterOpts,
		StopAfter:  pipeline.StopAfterFilter,
	}

	result, err := pipeline.New(opts).Run(ctx, args)
	if err != nil {
		return fmt.Errorf("cmd: pipeline: %w", err)
	}

	switch effectiveFormat {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result.Stats); err != nil {
			return fmt.Errorf("cmd: stats json: %w", err)
		}
	default:
		printStatsText(os.Stdout, result.Stats, result)
	}

	return nil
}

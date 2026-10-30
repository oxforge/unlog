package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/internal/config"
)

var (
	// Global state.
	verbose    bool
	noColor    bool
	configFile string
	cfg        config.Config

	// Analyze flags.
	levelFlag     string
	sinceFlag     string
	untilFlag     string
	noiseFileFlag string
	formatFlag    string
	outputFlag    string
	tzFlag        string
)

var rootCmd = &cobra.Command{
	Use:   "unlog [files...]",
	Short: "Unravel your logs",
	Long:  "CLI tool that ingests raw log files, preprocesses them to extract signal from noise,\nthen compacts them into a deterministic incident summary.",
	Args:  cobra.ArbitraryArgs,
	RunE:  runAnalyze,
	// Execute prints the error once; usage is for --help.
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		path := configFile
		if path == "" {
			home, err := os.UserHomeDir()
			if err == nil {
				path = filepath.Join(home, ".unlog", "config.toml")
			}
		}

		loaded, err := config.Load(path)
		if err != nil {
			return fmt.Errorf("cmd: %w", err)
		}
		cfg = loaded
		if len(cfg.Retired) > 0 {
			slog.Warn("unlog no longer calls LLMs; ignoring " + strings.Join(cfg.Retired, ", ") + " (see \"For agents\" in the README)")
		}

		if cmd.Flags().Changed("verbose") {
			cfg.Verbose = verbose
		} else if cfg.Verbose {
			verbose = cfg.Verbose
		}
		if verbose {
			// Debug messages say which files were skipped and why.
			slog.SetLogLoggerLevel(slog.LevelDebug)
		}
		if cmd.Flags().Changed("no-color") {
			cfg.NoColor = noColor
		} else if cfg.NoColor {
			noColor = cfg.NoColor
		}

		if _, ok := os.LookupEnv("NO_COLOR"); ok {
			noColor = true
			cfg.NoColor = true
		}

		return nil
	},
}

// Exit codes: 0 the report was written, 1 an error and no report, 130
// interrupted.
func Execute() {
	err := rootCmd.Execute()
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		os.Exit(130)
	}
	_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

func init() {
	// Global flags.
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Config file path (default: ~/.unlog/config.toml)")

	// Analyze flags.
	rootCmd.Flags().StringVar(&levelFlag, "level", "", "Minimum log level: trace, debug, info, warn, error, fatal")
	rootCmd.Flags().StringVar(&sinceFlag, "since", "", "Start time filter (ISO 8601 or relative: \"2h\", \"30m\")")
	rootCmd.Flags().StringVar(&untilFlag, "until", "", "End time filter (ISO 8601 or relative: \"2h\", \"30m\")")
	rootCmd.Flags().StringVar(&noiseFileFlag, "noise-file", "", "Path to custom noise patterns file")
	rootCmd.Flags().StringVar(&formatFlag, "format", "", "Output format: text, json, markdown (default: text)")
	rootCmd.Flags().StringVar(&outputFlag, "output", "", "Write output to file instead of stdout")
	rootCmd.Flags().StringVar(&tzFlag, "tz", "", "Time zone of timestamps that carry none, e.g. \"Europe/Berlin\" or \"Local\" (default: UTC); also applies to --since/--until")

	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w (see '%s --help')", err, c.CommandPath())
	})

	analyzeCmd.Flags().AddFlagSet(rootCmd.Flags())
	rootCmd.AddCommand(analyzeCmd)
}

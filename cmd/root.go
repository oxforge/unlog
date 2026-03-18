package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
	levelFlag      string
	sinceFlag      string
	untilFlag      string
	noiseFileFlag  string
	formatFlag     string
	outputFlag     string
	aiProviderFlag string
	modelFlag      string
)

var rootCmd = &cobra.Command{
	Use:   "unlog [files...]",
	Short: "Unravel your logs",
	Long:  "CLI tool that ingests raw log files, preprocesses them to extract signal from noise,\nthen optionally uses LLM APIs to produce incident timelines and root cause analysis.",
	Args:  cobra.ArbitraryArgs,
	RunE:  runAnalyze,
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
			return fmt.Errorf("cmd: load config: %w", err)
		}
		cfg = loaded

		if cmd.Flags().Changed("verbose") {
			cfg.Verbose = verbose
		} else if cfg.Verbose {
			verbose = cfg.Verbose
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

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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
	rootCmd.Flags().StringVar(&aiProviderFlag, "ai-provider", "", "Enable LLM analysis with provider: openai, anthropic, ollama")
	rootCmd.Flags().StringVar(&modelFlag, "model", "", "LLM model override (default per provider)")
}

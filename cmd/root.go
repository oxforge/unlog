package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/internal/config"
)

var (
	verbose    bool
	noColor    bool
	configFile string
	cfg        config.Config
)

var rootCmd = &cobra.Command{
	Use:   "unlog [files...]",
	Short: "Unravel your logs — runs 'analyze' by default",
	Long:  "CLI tool that ingests raw log files, preprocesses them to extract signal from noise,\nthen uses LLM APIs to produce structured incident timelines and root cause analysis.\n\nRunning 'unlog [files...]' is equivalent to 'unlog analyze [files...]'.",
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
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Config file path (default: ~/.unlog/config.toml)")

	registerAnalyzeFlags(rootCmd)
}

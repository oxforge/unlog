package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
)

var (
	// analyze-specific flags
	levelFlag      string
	sinceFlag      string
	untilFlag      string
	noiseFileFlag  string
	formatFlag     string
	outputFlag     string
	aiProviderFlag string
	modelFlag      string
	fastFlag       bool
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [files...]",
	Short: "Analyze log files and produce an incident timeline with root cause analysis",
	RunE:  runAnalyze,
}

// registerAnalyzeFlags adds analyze-specific flags to the given command.
func registerAnalyzeFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&levelFlag, "level", "", "Minimum log level: trace, debug, info, warn, error, fatal")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "Start time filter (ISO 8601 or relative: \"2h\", \"30m\")")
	cmd.Flags().StringVar(&untilFlag, "until", "", "End time filter (ISO 8601 or relative: \"2h\", \"30m\")")
	cmd.Flags().StringVar(&noiseFileFlag, "noise-file", "", "Path to custom noise patterns file")
	cmd.Flags().StringVar(&formatFlag, "format", "", "Output format: text, json, markdown (default: text)")
	cmd.Flags().StringVar(&outputFlag, "output", "", "Write output to file instead of stdout")
	cmd.Flags().StringVar(&aiProviderFlag, "ai-provider", "", "Enable LLM analysis with provider: openai, anthropic, ollama")
	cmd.Flags().StringVar(&modelFlag, "model", "", "LLM model override (default per provider)")
	cmd.Flags().BoolVar(&fastFlag, "fast", false, "Single-pass LLM analysis (faster, less detailed)")
}

func init() {
	registerAnalyzeFlags(analyzeCmd)
	rootCmd.AddCommand(analyzeCmd)
}

func runAnalyze(cmd *cobra.Command, args []string) (err error) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	effectiveFormat, err := resolveFormat(cmd, formatFlag)
	if err != nil {
		return err
	}

	filterOpts, err := buildFilterOpts(cmd, levelFlag, sinceFlag, untilFlag, noiseFileFlag, args)
	if err != nil {
		return err
	}

	opts := pipeline.Options{
		FilterOpts: filterOpts,
	}

	result, err := pipeline.New(opts).Run(ctx, args)
	if err != nil {
		return fmt.Errorf("cmd: pipeline: %w", err)
	}

	effectiveProvider := cfg.AIProvider
	if cmd.Flags().Changed("ai-provider") {
		effectiveProvider = aiProviderFlag
	}

	var ar *analyze.AnalysisResult

	if effectiveProvider != "" {
		effectiveModel := cfg.Model
		if cmd.Flags().Changed("model") {
			effectiveModel = modelFlag
		}

		provider, err := resolveProvider(effectiveProvider, effectiveModel)
		if err != nil {
			return err
		}

		var streamCB analyze.StreamCallback
		if effectiveFormat == "text" && outputFlag == "" {
			lastPass := analyze.Pass(-1)
			streamCB = func(pass analyze.Pass, token string) {
				if pass != lastPass {
					if lastPass >= 0 {
						_, _ = fmt.Fprintln(os.Stdout)
					}
					_, _ = fmt.Fprintf(os.Stdout, "\n--- %s ---\n", passHeader(pass))
					lastPass = pass
				}
				_, _ = fmt.Fprint(os.Stdout, token)
			}
		}

		analyzeOpts := analyze.Options{Fast: fastFlag}
		ar, err = analyze.Run(ctx, provider, result.Summary, analyzeOpts, streamCB)
		if err != nil {
			return fmt.Errorf("cmd: analyze: %w", err)
		}

		if effectiveFormat == "text" && outputFlag == "" {
			_, _ = fmt.Fprintln(os.Stdout)
		}
	}

	var out io.Writer = os.Stdout
	if outputFlag != "" {
		var f *os.File
		f, err = os.Create(outputFlag)
		if err != nil {
			return fmt.Errorf("cmd: create output file: %w", err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("cmd: close output file: %w", cerr)
			}
		}()
		out = f
	}

	effectiveNoColor := noColor
	if outputFlag != "" {
		effectiveNoColor = true
	} else if !noColor {
		effectiveNoColor = !isTerminal(os.Stdout)
	}

	var r render.Renderer
	switch effectiveFormat {
	case "json":
		r = &render.JSONRenderer{}
	case "markdown":
		r = &render.MarkdownRenderer{}
	default:
		r = &render.TerminalRenderer{}
	}

	renderOpts := render.Options{
		Result:   result,
		Analysis: ar,
		Version:  Version,
		NoColor:  effectiveNoColor,
		Verbose:  verbose,
	}

	if effectiveFormat == "text" && ar != nil && outputFlag == "" {
		// AI sections already streamed to stdout.
	} else {
		if err := r.Render(out, renderOpts); err != nil {
			return fmt.Errorf("cmd: render: %w", err)
		}
	}

	if effectiveFormat == "text" {
		printStats(os.Stderr, result, verbose)
	}

	return nil
}

// isTerminal reports whether f is connected to a terminal.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// passHeader returns a human-readable header for each analysis pass.
func passHeader(p analyze.Pass) string {
	switch p {
	case analyze.PassTimeline:
		return "Timeline"
	case analyze.PassRootCause:
		return "Root Cause"
	case analyze.PassRecommendations:
		return "Recommendations"
	default:
		return "Analysis"
	}
}

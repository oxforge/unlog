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

func runAnalyze(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 && isTerminal(os.Stdin) {
		return cmd.Help()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	effectiveFormat, err := resolveFormat(cmd)
	if err != nil {
		return err
	}

	filterOpts, err := buildFilterOpts(cmd, args)
	if err != nil {
		return err
	}

	result, err := pipeline.New(pipeline.Options{FilterOpts: filterOpts}).Run(ctx, args)
	if err != nil {
		return fmt.Errorf("cmd: pipeline: %w", err)
	}

	var ar *analyze.AnalysisResult

	if effectiveProvider := resolveProviderName(cmd); effectiveProvider != "" {
		provider, err := newProvider(effectiveProvider, resolveModelName(cmd))
		if err != nil {
			return err
		}

		var streamCB analyze.StreamCallback
		if effectiveFormat == "text" && outputFlag == "" {
			streamCB = func(token string) {
				_, _ = fmt.Fprint(os.Stdout, token)
			}
		}

		ar, err = analyze.Run(ctx, provider, result.Summary, streamCB)
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

	r := newRenderer(effectiveFormat)
	renderOpts := render.Options{
		Result:   result,
		Analysis: ar,
		Version:  Version,
		NoColor:  effectiveNoColor,
		Verbose:  verbose,
	}

	if effectiveFormat == "text" && ar != nil && outputFlag == "" {
		// AI output already streamed to stdout.
	} else {
		if err := r.Render(out, renderOpts); err != nil {
			return fmt.Errorf("cmd: render: %w", err)
		}
	}

	if effectiveFormat == "text" {
		printStats(os.Stderr, result, ar, verbose)
	}

	return nil
}

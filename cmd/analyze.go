package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/oxforge/unlog/ingest"
	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
	"github.com/oxforge/unlog/internal/render"
)

// analyzeCmd is what bare unlog runs, under its own name: `unlog analyze
// logs/` and `unlog logs/` are the same. Wrappers (the enterprise CLI) call it
// by name. It shares the root command's flags, registered in root.go.
var analyzeCmd = &cobra.Command{
	Use:   "analyze [files...]",
	Short: "Analyze log files (the default command)",
	Args:  cobra.ArbitraryArgs,
	RunE:  runAnalyze,
}

func runAnalyze(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 && isTerminal(os.Stdin) {
		return cmd.Help()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			slog.Debug("cmd: received signal, shutting down gracefully...")
			cancel()
		case <-ctx.Done():
			return
		}
		select {
		case <-sigCh:
			slog.Debug("cmd: received second signal, forcing exit")
			os.Exit(1)
		case <-ctx.Done():
		}
	}()
	defer signal.Stop(sigCh)

	effectiveFormat, err := resolveFormat(cmd)
	if err != nil {
		return err
	}

	aiTimeout, err := resolveAITimeout(cmd)
	if err != nil {
		return err
	}

	loc, err := resolveLocation(cmd)
	if err != nil {
		return err
	}

	filterOpts, err := buildFilterOpts(cmd, loc)
	if err != nil {
		return err
	}

	// Check the AI provider (name, API key) before reading any input.
	var provider analyze.Provider
	if name := resolveProviderName(cmd); name != "" {
		if provider, err = newProvider(name, resolveModelName(cmd), aiTimeout); err != nil {
			return err
		}
	}

	// Open --output before reading input or paying for an AI call. It is
	// truncated only when the report is written; a file this run created is
	// removed if the run fails before that. It is never read as input.
	var outFile *os.File
	written := false
	if outputFlag != "" {
		if err := checkNotAnInput(outputFlag, args); err != nil {
			return err
		}
		_, statErr := os.Stat(outputFlag)
		created := errors.Is(statErr, fs.ErrNotExist)
		outFile, err = os.OpenFile(outputFlag, os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("cmd: open output file: %w", err)
		}
		defer func() {
			if cerr := outFile.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("cmd: close output file: %w", cerr)
			}
			if created && !written {
				_ = os.Remove(outputFlag)
			}
		}()
	}

	result, err := pipeline.New(pipeline.Options{
		IngestOpts: ingest.IngestOptions{Location: loc, Exclude: excludedInputs()},
		FilterOpts: filterOpts,
	}).Run(ctx, args)
	if err != nil {
		return fmt.Errorf("cmd: %w", err)
	}

	var ar *analyze.AnalysisResult
	var aiErr error
	if provider != nil {
		var streamCB analyze.StreamCallback
		if effectiveFormat == "text" {
			streamW := os.Stdout
			if outputFlag != "" {
				streamW = os.Stderr
			}
			streamCB = func(token string) {
				_, _ = fmt.Fprint(streamW, token)
			}
		}

		// A failed analysis still gets the report written below, with the
		// error in it; the run then exits with exitAIFailed.
		ar, aiErr = analyze.Run(ctx, provider, result.Summary, cfg.SystemPrompt, streamCB)
		if aiErr != nil {
			if ar == nil {
				ar = &analyze.AnalysisResult{ModelUsed: provider.Model()}
			}
			ar.Error = aiErr.Error()
		}

		if effectiveFormat == "text" {
			streamW := os.Stdout
			if outputFlag != "" {
				streamW = os.Stderr
			}
			_, _ = fmt.Fprintln(streamW)
		}
	}

	var out io.Writer = os.Stdout
	if outFile != nil {
		// Only a regular file has old content to drop; a device or a pipe
		// (/dev/stdout, >(cat)) can't be truncated.
		if info, err := outFile.Stat(); err == nil && info.Mode().IsRegular() {
			if err := outFile.Truncate(0); err != nil {
				return fmt.Errorf("cmd: truncate output file: %w", err)
			}
		}
		out, written = outFile, true
	}

	effectiveNoColor := noColor
	if outputFlag != "" {
		effectiveNoColor = true
	} else if !noColor {
		effectiveNoColor = !isTerminal(os.Stdout)
	}

	aiStreamed := effectiveFormat == "text" && ar != nil && outputFlag == ""
	r := newRenderer(effectiveFormat)
	renderOpts := render.Options{
		Result:     result,
		Analysis:   ar,
		Version:    version(),
		NoColor:    effectiveNoColor,
		AIStreamed: aiStreamed,
	}

	if err := r.Render(out, renderOpts); err != nil {
		return fmt.Errorf("cmd: %w", err)
	}

	if effectiveFormat == "text" {
		printStats(os.Stderr, result, ar, verbose)
	}

	if aiErr != nil {
		return &exitError{code: exitAIFailed, err: fmt.Errorf("cmd: %w", aiErr)}
	}
	return nil
}

// checkNotAnInput fails when output is one of the files named in args, which
// writing the report would overwrite.
func checkNotAnInput(output string, args []string) error {
	out, err := os.Stat(output)
	if err != nil {
		return nil // not there yet, so no input is it
	}
	for _, arg := range args {
		if in, err := os.Stat(arg); err == nil && os.SameFile(in, out) {
			return fmt.Errorf("cmd: --output %s is one of the input files", output)
		}
	}
	return nil
}

// excludedInputs lists the files the ingest stage must not read: the report
// being written, which may sit in a directory being searched.
func excludedInputs() []string {
	if outputFlag == "" {
		return nil
	}
	return []string{outputFlag}
}

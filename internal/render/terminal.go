package render

import (
	"fmt"
	"io"
	"strings"
)

const (
	ansiReset    = "\033[0m"
	ansiBold     = "\033[1m"
	ansiDim      = "\033[2m"
	ansiRed      = "\033[31m"
	ansiGreen    = "\033[32m"
	ansiYellow   = "\033[33m"
	ansiCyan     = "\033[36m"
	ansiBoldCyan = "\033[1;36m"
)

var levelReplacer = strings.NewReplacer(
	"ERROR", ansiRed+"ERROR"+ansiReset,
	"FATAL", ansiRed+"FATAL"+ansiReset,
	"WARN", ansiYellow+"WARN"+ansiReset,
	"INFO", ansiGreen+"INFO"+ansiReset,
)

type TerminalRenderer struct{}

func (r *TerminalRenderer) Render(w io.Writer, opts Options) error {
	ew := &errWriter{w: w}
	c := colorizer{enabled: !opts.NoColor}

	r.renderSummary(ew, opts, c)
	switch {
	case opts.Analysis == nil:
	case !opts.AIStreamed:
		r.renderAI(ew, opts, c)
	case opts.Analysis.Error != "":
		// The analysis was streamed above; say that it was cut short.
		_, _ = fmt.Fprintf(ew, "\nAI analysis failed: %s\n", opts.Analysis.Error)
	}

	return ew.err
}

func (r *TerminalRenderer) renderSummary(w io.Writer, opts Options, c colorizer) {
	if opts.Result == nil || opts.Result.Summary == "" {
		return
	}

	lines := strings.Split(opts.Result.Summary, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			_, _ = fmt.Fprintln(w, c.wrap(ansiBoldCyan, line))
		} else {
			_, _ = fmt.Fprintln(w, c.highlightLevels(line))
		}
	}
}

func (r *TerminalRenderer) renderAI(w io.Writer, opts Options, c colorizer) {
	body := strings.TrimRight(opts.Analysis.Analysis, "\n")
	if opts.Analysis.Error != "" {
		body = strings.TrimSpace(body + "\n\nAI analysis failed: " + opts.Analysis.Error)
	}
	if body == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "\n%s\n", c.wrap(ansiBoldCyan, "--- Analysis ---"))
	_, _ = fmt.Fprintln(w, c.highlightLevels(body))
	_, _ = fmt.Fprintln(w)
}

type colorizer struct {
	enabled bool
}

func (c colorizer) wrap(code, text string) string {
	if !c.enabled {
		return text
	}
	return code + text + ansiReset
}

func (c colorizer) highlightLevels(line string) string {
	if !c.enabled {
		return line
	}
	return levelReplacer.Replace(line)
}

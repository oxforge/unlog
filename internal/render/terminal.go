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

	if opts.Analysis != nil {
		r.renderAI(ew, opts, c)
	} else {
		r.renderNoAI(ew, opts, c)
	}

	return ew.err
}

func (r *TerminalRenderer) renderNoAI(w io.Writer, opts Options, c colorizer) {
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
	sections := []struct {
		header string
		body   string
	}{
		{"Timeline", opts.Analysis.Timeline},
		{"Root Cause", opts.Analysis.RootCause},
		{"Recommendations", opts.Analysis.Recommendations},
	}

	for _, s := range sections {
		if s.body == "" {
			continue
		}
		_, _ = fmt.Fprintf(w, "\n%s\n", c.wrap(ansiBoldCyan, "--- "+s.header+" ---"))
		_, _ = fmt.Fprintln(w, c.highlightLevels(strings.TrimRight(s.body, "\n")))
		_, _ = fmt.Fprintln(w)
	}
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

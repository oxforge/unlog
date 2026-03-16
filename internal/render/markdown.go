package render

import (
	"fmt"
	"io"
	"time"
)

type MarkdownRenderer struct{}

func (r *MarkdownRenderer) Render(w io.Writer, opts Options) error {
	ew := &errWriter{w: w}

	_, _ = fmt.Fprintln(ew, "# Incident Analysis")
	_, _ = fmt.Fprintln(ew)

	meta := fmt.Sprintf("**Generated**: %s | **Version**: %s",
		time.Now().UTC().Format(time.RFC3339), opts.Version)
	if opts.Analysis != nil && opts.Analysis.ModelUsed != "" {
		meta += fmt.Sprintf(" | **Model**: %s", opts.Analysis.ModelUsed)
	}
	_, _ = fmt.Fprintln(ew, meta)

	if opts.Analysis != nil {
		writeMDSection(ew, "Timeline", opts.Analysis.Timeline)
		writeMDSection(ew, "Root Cause", opts.Analysis.RootCause)
		writeMDSection(ew, "Recommendations", opts.Analysis.Recommendations)
	}

	if opts.Result != nil && opts.Result.Summary != "" {
		writeMDSection(ew, "Summary", opts.Result.Summary)
	}

	if opts.Result != nil {
		r.renderStatsTable(ew, opts)
	}

	return ew.err
}

func writeMDSection(w io.Writer, header, body string) {
	if body == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "\n## %s\n\n%s\n", header, body)
}

func (r *MarkdownRenderer) renderStatsTable(w io.Writer, opts Options) {
	fs := opts.Result.Stats

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "## Statistics")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "| Metric | Value |")
	_, _ = fmt.Fprintln(w, "|--------|-------|")
	_, _ = fmt.Fprintf(w, "| Ingested | %s |\n", FmtIntComma(fs.TotalIngested))
	_, _ = fmt.Fprintf(w, "| Dropped | %s |\n", FmtIntComma(fs.TotalDropped))
	_, _ = fmt.Fprintf(w, "| Survived | %s |\n", FmtIntComma(fs.TotalSurvived))
	_, _ = fmt.Fprintf(w, "| Unique signatures | %d |\n", fs.UniqueSignatures)
	_, _ = fmt.Fprintf(w, "| Duration | %dms |\n", opts.Result.Duration.Milliseconds())
	if opts.Analysis != nil {
		_, _ = fmt.Fprintf(w, "| AI analysis | %.1fs |\n", opts.Analysis.Duration.Seconds())
	}
}

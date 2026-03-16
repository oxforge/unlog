// Package render provides output formatters for pipeline results.
package render

import (
	"fmt"
	"io"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/pipeline"
)

type Options struct {
	Result   *pipeline.Result
	Analysis *analyze.AnalysisResult
	Version  string
	NoColor  bool
	Verbose  bool
}

type Renderer interface {
	Render(w io.Writer, opts Options) error
}

type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) Write(p []byte) (int, error) {
	if ew.err != nil {
		return 0, ew.err
	}
	n, err := ew.w.Write(p)
	ew.err = err
	return n, err
}

// FmtIntComma formats an int64 with comma separators (e.g., 10000 → "10,000").
func FmtIntComma(n int64) string {
	if n < 0 {
		return "-" + FmtIntComma(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return FmtIntComma(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}

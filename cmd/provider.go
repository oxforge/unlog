package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/oxforge/unlog/internal/analyze"
	"github.com/oxforge/unlog/internal/render"
)

// newProvider creates an LLM provider from a provider name and optional model override.
func newProvider(name, model string, timeout time.Duration) (analyze.Provider, error) {
	switch name {
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: OPENAI_API_KEY not set")
		}
		return analyze.NewOpenAI(key, model, "", timeout)
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: ANTHROPIC_API_KEY not set")
		}
		return analyze.NewAnthropic(key, model, "", timeout)
	case "gemini":
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("cmd: GEMINI_API_KEY not set")
		}
		return analyze.NewGemini(key, model, "", timeout)
	case "ollama":
		return analyze.NewOllama("", model, timeout), nil
	default:
		return nil, fmt.Errorf("cmd: unknown AI provider: %q (valid: openai, anthropic, gemini, ollama)", name)
	}
}

// newRenderer returns a renderer for the given format.
func newRenderer(format string) render.Renderer {
	switch format {
	case "json":
		return &render.JSONRenderer{}
	case "markdown":
		return &render.MarkdownRenderer{}
	default:
		return &render.TerminalRenderer{}
	}
}

package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSummary = "## Incident Overview\nDatabase connection pool exhausted.\n5 errors, 2 warnings over 90 seconds."

// mockOpenAIServer returns an httptest.Server that responds with
// SSE events in OpenAI's Chat Completions format.
func mockOpenAIServer(response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		words := strings.Fields(response)
		for i, word := range words {
			if i > 0 {
				word = " " + word
			}
			chunk := openaiChunk{
				Choices: []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				}{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: word}},
				},
			}
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
}

func TestIntegrationOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		resp := "Timeline: pool exhausted. Root cause: connection leak. Fix: add monitoring."
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, word := range strings.Fields(resp) {
			chunk := ollamaChunk{Response: word + " ", Done: false}
			b, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "%s\n", b)
		}
		done := ollamaChunk{Done: true}
		b, _ := json.Marshal(done)
		_, _ = fmt.Fprintf(w, "%s\n", b)
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "llama3", 0)
	result, err := Run(context.Background(), p, testSummary, "", nil)
	require.NoError(t, err)

	assert.Contains(t, result.Analysis, "Timeline:")
	assert.Contains(t, result.Analysis, "Root cause:")
	assert.Equal(t, "llama3", result.ModelUsed)
	assert.True(t, result.Duration > 0)
}

func TestIntegrationOpenAI(t *testing.T) {
	srv := mockOpenAIServer("Timeline: connection errors. Root cause: missing limits. Recommendation: set max_connections.")
	defer srv.Close()

	p, err := NewOpenAI("test-key", "gpt-4o", srv.URL, 0)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, "", nil)
	require.NoError(t, err)

	assert.Contains(t, result.Analysis, "Timeline:")
	assert.Contains(t, result.Analysis, "Root cause:")
	assert.Equal(t, "gpt-4o", result.ModelUsed)
	assert.True(t, result.Duration > 0)
}

func TestIntegrationAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		resp := "Timeline: pool exhaustion. Root cause: unbounded connections. Recommendation: circuit breaker."
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, word := range strings.Fields(resp) {
			delta := anthropicDelta{}
			delta.Delta.Text = word + " "
			data, _ := json.Marshal(delta)
			_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", data)
			flusher.Flush()
		}
		_, _ = fmt.Fprintf(w, "event: message_stop\ndata: {}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p, err := NewAnthropic("test-key", "claude-sonnet-4-20250514", srv.URL, 0)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, "", nil)
	require.NoError(t, err)

	assert.Contains(t, result.Analysis, "Timeline:")
	assert.Contains(t, result.Analysis, "Root cause:")
	assert.Equal(t, "claude-sonnet-4-20250514", result.ModelUsed)
	assert.True(t, result.Duration > 0)
}

func TestIntegrationGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v1beta/models/gemini-2.5-flash:streamGenerateContent")
		assert.Equal(t, "test-key", r.URL.Query().Get("key"))

		resp := "Timeline: pool exhaustion. Root cause: connection leak. Recommendation: add pool limits."
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, word := range strings.Fields(resp) {
			chunk := map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{
								{"text": word + " "},
							},
						},
					},
				},
			}
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p, err := NewGemini("test-key", "gemini-2.5-flash", srv.URL, 0)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, "", nil)
	require.NoError(t, err)

	assert.Contains(t, result.Analysis, "Timeline:")
	assert.Contains(t, result.Analysis, "Root cause:")
	assert.Equal(t, "gemini-2.5-flash", result.ModelUsed)
	assert.True(t, result.Duration > 0)
}

func TestIntegrationProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p, err := NewOpenAI("test-key", "gpt-4o", srv.URL, 0)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, "", nil)
	require.Error(t, err)
	assert.NotNil(t, result)
}

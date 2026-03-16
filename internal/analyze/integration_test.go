package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestIntegrationOllamaMultiPass(t *testing.T) {
	var callCount atomic.Int32
	responses := []string{
		"Timeline: DB pool exhausted at T+30s",
		"Root cause: connection leak in payment service",
		"Fix: add connection pool monitoring and alerts",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		idx := int(callCount.Load())

		// Verify chaining: pass 2+ prompts should contain previous output.
		if idx == 1 {
			assert.Contains(t, req.Prompt, "Timeline: DB pool exhausted")
		}
		if idx == 2 {
			assert.Contains(t, req.Prompt, "Timeline: DB pool exhausted")
			assert.Contains(t, req.Prompt, "Root cause: connection leak")
		}

		resp := responses[idx]
		callCount.Add(1)

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

	p := NewOllama(srv.URL, "llama3")
	result, err := Run(context.Background(), p, testSummary, Options{}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Timeline, "Timeline:")
	assert.Contains(t, result.RootCause, "Root cause:")
	assert.Contains(t, result.Recommendations, "Fix:")
	assert.Equal(t, "llama3", result.ModelUsed)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestIntegrationOpenAIMultiPass(t *testing.T) {
	var callCount atomic.Int32
	responses := []string{
		"Timeline: connection errors started at 10:31",
		"Root cause: missing connection pool limits",
		"Recommendation: set max_connections=100",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		idx := int(callCount.Load())

		// Verify chaining.
		if idx == 1 {
			assert.Contains(t, req.Messages[1].Content, "Timeline: connection errors")
		}
		if idx == 2 {
			assert.Contains(t, req.Messages[1].Content, "Timeline: connection errors")
			assert.Contains(t, req.Messages[1].Content, "Root cause: missing connection")
		}

		resp := responses[idx]
		callCount.Add(1)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, word := range strings.Fields(resp) {
			chunk := openaiChunk{
				Choices: []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				}{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: word + " "}},
				},
			}
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p, err := NewOpenAI("test-key", "gpt-4o", srv.URL)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, Options{}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Timeline, "Timeline:")
	assert.Contains(t, result.RootCause, "Root cause:")
	assert.Contains(t, result.Recommendations, "Recommendation:")
	assert.Equal(t, "gpt-4o", result.ModelUsed)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestIntegrationAnthropicMultiPass(t *testing.T) {
	var callCount atomic.Int32
	responses := []string{
		"Timeline: pool exhaustion cascade",
		"Root cause: unbounded connection creation",
		"Recommendation: implement circuit breaker",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		idx := int(callCount.Load())

		// Verify chaining.
		if idx == 1 {
			assert.Contains(t, req.Messages[0].Content, "Timeline: pool exhaustion")
		}
		if idx == 2 {
			assert.Contains(t, req.Messages[0].Content, "Timeline: pool exhaustion")
			assert.Contains(t, req.Messages[0].Content, "Root cause: unbounded")
		}

		resp := responses[idx]
		callCount.Add(1)

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

	p, err := NewAnthropic("test-key", "claude-sonnet-4-20250514", srv.URL)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, Options{}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Timeline, "Timeline:")
	assert.Contains(t, result.RootCause, "Root cause:")
	assert.Contains(t, result.Recommendations, "Recommendation:")
	assert.Equal(t, "claude-sonnet-4-20250514", result.ModelUsed)
	assert.Equal(t, int32(3), callCount.Load())
}

func TestIntegrationFastMode(t *testing.T) {
	srv := mockOpenAIServer("Combined: timeline, root cause, and recommendations all in one pass")
	defer srv.Close()

	p, err := NewOpenAI("test-key", "gpt-4o", srv.URL)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, Options{Fast: true}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Timeline, "Combined:")
	// Fast mode stores same output in all three fields.
	assert.Equal(t, result.Timeline, result.RootCause)
	assert.Equal(t, result.Timeline, result.Recommendations)
}

func TestIntegrationProviderError(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := callCount.Add(1)
		if idx == 2 {
			// Fail on second call (root cause pass).
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunk := openaiChunk{
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{
				{Delta: struct {
					Content string `json:"content"`
				}{Content: "pass 1 output"}},
			},
		}
		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p, err := NewOpenAI("test-key", "gpt-4o", srv.URL)
	require.NoError(t, err)

	result, err := Run(context.Background(), p, testSummary, Options{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root cause pass")

	// Pass 1 result should still be available.
	assert.Equal(t, "pass 1 output", result.Timeline)
}

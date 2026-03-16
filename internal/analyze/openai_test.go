package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req openaiRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "gpt-4o", req.Model)
		assert.True(t, req.Stream)
		assert.Len(t, req.Messages, 2)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " world", "!"}
		for _, tok := range tokens {
			chunk := openaiChunk{
				Choices: []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				}{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: tok}},
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

	tokenCh, errCh := p.Analyze(context.Background(), "system", "prompt")

	var tokens []string
	for tok := range tokenCh {
		tokens = append(tokens, tok)
	}

	assert.Equal(t, []string{"Hello", " world", "!"}, tokens)

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	default:
	}
}

func TestOpenAIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p, err := NewOpenAI("bad-key", "", srv.URL)
	require.NoError(t, err)

	tokenCh, errCh := p.Analyze(context.Background(), "sys", "prompt")
	for range tokenCh {
	}

	e := <-errCh
	require.Error(t, e)
	assert.Contains(t, e.Error(), "HTTP 401")
}

func TestOpenAIEmptyKey(t *testing.T) {
	_, err := NewOpenAI("", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key is required")
}

func TestOpenAIContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				}{Content: "first"}},
			},
		}
		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p, err := NewOpenAI("test-key", "", srv.URL)
	require.NoError(t, err)

	tokenCh, errCh := p.Analyze(ctx, "sys", "prompt")

	tok := <-tokenCh
	assert.Equal(t, "first", tok)

	cancel()
	for range tokenCh {
	}

	select {
	case err := <-errCh:
		if err != nil {
			assert.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error channel")
	}
}

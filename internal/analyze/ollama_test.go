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

func TestOllamaStreaming(t *testing.T) {
	chunks := []ollamaChunk{
		{Response: "Hello", Done: false},
		{Response: " world", Done: false},
		{Response: "!", Done: false},
		{Response: "", Done: true},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/generate", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)

		var req ollamaRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "llama3", req.Model)
		assert.True(t, req.Stream)

		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, c := range chunks {
			b, _ := json.Marshal(c)
			_, _ = fmt.Fprintf(w, "%s\n", b)
		}
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "llama3")
	tokenCh, errCh := p.Analyze(context.Background(), "system prompt", "user prompt")

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

func TestOllamaHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewOllama(srv.URL, "llama3")
	tokenCh, errCh := p.Analyze(context.Background(), "sys", "prompt")

	// Drain tokens (should be none).
	for range tokenCh {
	}

	e := <-errCh
	require.Error(t, e)
	assert.Contains(t, e.Error(), "HTTP 500")
}

func TestOllamaContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send one chunk, then block until client disconnects.
		chunk := ollamaChunk{Response: "first", Done: false}
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "%s\n", b)
		w.(http.Flusher).Flush()
		// Block until request context is done.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := NewOllama(srv.URL, "llama3")
	tokenCh, errCh := p.Analyze(ctx, "sys", "prompt")

	// Read the first token.
	tok := <-tokenCh
	assert.Equal(t, "first", tok)

	// Cancel and let it wind down.
	cancel()

	// Drain remaining.
	for range tokenCh {
	}

	// Error channel may have context error or be empty — both are fine.
	select {
	case err := <-errCh:
		if err != nil {
			assert.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error channel")
	}
}

func TestOllamaDefaultURL(t *testing.T) {
	p := NewOllama("", "")
	assert.Equal(t, "http://localhost:11434", p.baseURL)
	assert.Equal(t, "llama3", p.model)
	assert.Equal(t, "ollama", p.Name())
	assert.Equal(t, "llama3", p.Model())
}

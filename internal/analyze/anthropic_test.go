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

func TestAnthropicStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req anthropicRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "claude-sonnet-4-20250514", req.Model)
		assert.True(t, req.Stream)
		assert.Equal(t, 8192, req.MaxTokens)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " world", "!"}
		for _, tok := range tokens {
			delta := anthropicDelta{}
			delta.Delta.Text = tok
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

func TestAnthropicHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p, err := NewAnthropic("bad-key", "", srv.URL)
	require.NoError(t, err)

	tokenCh, errCh := p.Analyze(context.Background(), "sys", "prompt")
	for range tokenCh {
	}

	e := <-errCh
	require.Error(t, e)
	assert.Contains(t, e.Error(), "HTTP 401")
}

func TestAnthropicEmptyKey(t *testing.T) {
	_, err := NewAnthropic("", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key is required")
}

func TestAnthropicContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		delta := anthropicDelta{}
		delta.Delta.Text = "first"
		data, _ := json.Marshal(delta)
		_, _ = fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", data)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p, err := NewAnthropic("test-key", "", srv.URL)
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

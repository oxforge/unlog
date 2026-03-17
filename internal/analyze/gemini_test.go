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

func TestGeminiStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v1beta/models/gemini-2.5-flash:streamGenerateContent")
		assert.Equal(t, "sse", r.URL.Query().Get("alt"))
		assert.Equal(t, "test-key", r.URL.Query().Get("key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req geminiRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.NotNil(t, req.SystemInstruction)
		assert.Len(t, req.Contents, 1)
		assert.Equal(t, "user", req.Contents[0].Role)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		tokens := []string{"Hello", " world", "!"}
		for _, tok := range tokens {
			chunk := map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{
								{"text": tok},
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

	p, err := NewGemini("test-key", "gemini-2.5-flash", srv.URL)
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

func TestGeminiHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p, err := NewGemini("bad-key", "", srv.URL)
	require.NoError(t, err)

	tokenCh, errCh := p.Analyze(context.Background(), "sys", "prompt")
	for range tokenCh {
	}

	e := <-errCh
	require.Error(t, e)
	assert.Contains(t, e.Error(), "HTTP 401")
}

func TestGeminiEmptyKey(t *testing.T) {
	_, err := NewGemini("", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key is required")
}

func TestGeminiContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunk := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "first"},
						},
					},
				},
			},
		}
		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p, err := NewGemini("test-key", "", srv.URL)
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

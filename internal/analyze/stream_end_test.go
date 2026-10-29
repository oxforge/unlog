package analyze

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drain collects a provider's tokens and its error.
func drain(tokenCh <-chan string, errCh <-chan error) (string, error) {
	var sb strings.Builder
	for tok := range tokenCh {
		sb.WriteString(tok)
	}
	return sb.String(), <-errCh
}

// A stream that stops before the provider's end-of-stream event (a dropped
// connection, a proxy timeout) is an error, not a complete analysis.
func TestProvidersReportAStreamThatEndsEarly(t *testing.T) {
	tests := []struct {
		name  string
		first string // the one event the server sends before closing
		new   func(url string) Provider
	}{
		{"anthropic", "event: content_block_delta\ndata: {\"delta\":{\"text\":\"partial \"}}\n\n",
			func(url string) Provider { p, _ := NewAnthropic("k", "", url, time.Second); return p }},
		{"openai", "data: {\"choices\":[{\"delta\":{\"content\":\"partial \"}}]}\n\n",
			func(url string) Provider { p, _ := NewOpenAI("k", "", url, time.Second); return p }},
		{"gemini", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial \"}]}}]}\n\n",
			func(url string) Provider { p, _ := NewGemini("k", "", url, time.Second); return p }},
		{"ollama", "{\"response\":\"partial \",\"done\":false}\n",
			func(url string) Provider { return NewOllama(url, "", time.Second) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, tt.first)
				w.(http.Flusher).Flush()
			}))
			defer srv.Close()

			text, err := drain(tt.new(srv.URL).Analyze(context.Background(), "system", "prompt"))

			assert.Equal(t, "partial ", text)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "stream ended early")
		})
	}
}

// Cancelling mid-stream reports the cancellation, so Ctrl-C exits 130.
func TestProvidersReportCancellationMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"delta\":{\"text\":\"partial \"}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	p, err := NewAnthropic("k", "", srv.URL, time.Minute)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())

	tokenCh, errCh := p.Analyze(ctx, "system", "prompt")
	<-tokenCh
	cancel()
	_, err = drain(tokenCh, errCh)

	assert.ErrorIs(t, err, context.Canceled)
}

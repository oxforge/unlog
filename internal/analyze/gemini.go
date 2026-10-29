package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type GeminiProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewGemini creates a Google Gemini provider. Pass "" for default baseURL, 0 for default timeout.
func NewGemini(apiKey, model, baseURL string, timeout time.Duration) (*GeminiProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analyze: gemini: API key is required")
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &GeminiProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: timeout,
			// Prevent redirects from leaking the API key to other hosts.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (p *GeminiProvider) Name() string  { return "gemini" }
func (p *GeminiProvider) Model() string { return p.model }

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiChunk struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

func (p *GeminiProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		body, err := json.Marshal(geminiRequest{
			SystemInstruction: &geminiContent{
				Parts: []geminiPart{{Text: system}},
			},
			Contents: []geminiContent{
				{
					Role:  "user",
					Parts: []geminiPart{{Text: prompt}},
				},
			},
		})
		if err != nil {
			errCh <- fmt.Errorf("analyze: gemini: marshal request: %w", err)
			return
		}

		// The key goes in a header, not the URL: errors quote the URL, and
		// they end up in the report's analysis_error.
		url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", p.baseURL, p.model)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("analyze: gemini: create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-goog-api-key", p.apiKey)

		resp, err := p.client.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("analyze: gemini: request failed: %w", err)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			errCh <- fmt.Errorf("analyze: gemini: HTTP %d: %s", resp.StatusCode, string(body))
			return
		}

		// The last chunk carries a finish reason; without one the stream
		// was cut short.
		events, stopped := parseSSE(ctx, resp.Body)
		finished := false
		for ev := range events {
			var chunk geminiChunk
			if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
				errCh <- fmt.Errorf("analyze: gemini: parse chunk: %w", err)
				return
			}

			if len(chunk.Candidates) > 0 &&
				len(chunk.Candidates[0].Content.Parts) > 0 &&
				chunk.Candidates[0].Content.Parts[0].Text != "" {
				select {
				case tokenCh <- chunk.Candidates[0].Content.Parts[0].Text:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
			if len(chunk.Candidates) > 0 && chunk.Candidates[0].FinishReason != "" {
				finished = true
			}
		}
		if err := stopped(); err != nil || !finished {
			errCh <- fmt.Errorf("analyze: gemini: %w", streamEnd(stopped))
		}
	}()

	return tokenCh, errCh
}

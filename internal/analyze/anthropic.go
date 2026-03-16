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

type AnthropicProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewAnthropic creates an Anthropic provider. Pass "" for default baseURL.
func NewAnthropic(apiKey, model, baseURL string) (*AnthropicProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analyze: anthropic: API key is required")
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (p *AnthropicProvider) Name() string  { return "anthropic" }
func (p *AnthropicProvider) Model() string { return p.model }

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicDelta struct {
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

func (p *AnthropicProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		body, err := json.Marshal(anthropicRequest{
			Model:     p.model,
			MaxTokens: 8192,
			Stream:    true,
			System:    system,
			Messages: []anthropicMessage{
				{Role: "user", Content: prompt},
			},
		})
		if err != nil {
			errCh <- fmt.Errorf("analyze: anthropic: marshal request: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("analyze: anthropic: create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", p.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := p.client.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("analyze: anthropic: %w", err)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			errCh <- fmt.Errorf("analyze: anthropic: HTTP %d: %s", resp.StatusCode, string(body))
			return
		}

		events := parseSSE(ctx, resp.Body)
		for ev := range events {
			switch ev.Event {
			case "content_block_delta":
				var delta anthropicDelta
				if err := json.Unmarshal([]byte(ev.Data), &delta); err != nil {
					errCh <- fmt.Errorf("analyze: anthropic: parse delta: %w", err)
					return
				}
				if delta.Delta.Text != "" {
					select {
					case tokenCh <- delta.Delta.Text:
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					}
				}
			case "message_stop":
				return
			case "error":
				errCh <- fmt.Errorf("analyze: anthropic: stream error: %s", ev.Data)
				return
			}
		}
	}()

	return tokenCh, errCh
}

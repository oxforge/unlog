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

type OpenAIProvider struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewOpenAI creates an OpenAI provider. Pass "" for default baseURL.
func NewOpenAI(apiKey, model, baseURL string) (*OpenAIProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analyze: openai: API key is required")
	}
	if model == "" {
		model = "gpt-4o"
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	return &OpenAIProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (p *OpenAIProvider) Name() string  { return "openai" }
func (p *OpenAIProvider) Model() string { return p.model }

type openaiRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []openaiMessage `json:"messages"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func (p *OpenAIProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		body, err := json.Marshal(openaiRequest{
			Model:  p.model,
			Stream: true,
			Messages: []openaiMessage{
				{Role: "system", Content: system},
				{Role: "user", Content: prompt},
			},
		})
		if err != nil {
			errCh <- fmt.Errorf("analyze: openai: marshal request: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("analyze: openai: create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("analyze: openai: %w", err)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			errCh <- fmt.Errorf("analyze: openai: HTTP %d: %s", resp.StatusCode, string(body))
			return
		}

		events := parseSSE(ctx, resp.Body)
		for ev := range events {
			if ev.Data == "[DONE]" {
				return
			}

			var chunk openaiChunk
			if err := json.Unmarshal([]byte(ev.Data), &chunk); err != nil {
				errCh <- fmt.Errorf("analyze: openai: parse chunk: %w", err)
				return
			}

			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				select {
				case tokenCh <- chunk.Choices[0].Delta.Content:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}
	}()

	return tokenCh, errCh
}

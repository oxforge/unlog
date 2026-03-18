package analyze

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllama creates an Ollama provider with defaults for empty baseURL/model, 0 for default timeout.
func NewOllama(baseURL, model string, timeout time.Duration) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = "llama3"
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &OllamaProvider{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *OllamaProvider) Name() string  { return "ollama" }
func (p *OllamaProvider) Model() string { return p.model }

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system"`
	Stream bool   `json:"stream"`
}

type ollamaChunk struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (p *OllamaProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		body, err := json.Marshal(ollamaRequest{
			Model:  p.model,
			Prompt: prompt,
			System: system,
			Stream: true,
		})
		if err != nil {
			errCh <- fmt.Errorf("analyze: ollama: marshal request: %w", err)
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/generate", bytes.NewReader(body))
		if err != nil {
			errCh <- fmt.Errorf("analyze: ollama: create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := p.client.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("analyze: ollama: %w", err)
			return
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			errCh <- fmt.Errorf("analyze: ollama: HTTP %d: %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			var chunk ollamaChunk
			if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
				errCh <- fmt.Errorf("analyze: ollama: parse chunk: %w", err)
				return
			}

			if chunk.Done {
				return
			}

			if chunk.Response != "" {
				select {
				case tokenCh <- chunk.Response:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("analyze: ollama: read stream: %w", err)
		}
	}()

	return tokenCh, errCh
}

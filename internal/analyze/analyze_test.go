package analyze

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name     string
	model    string
	response string
	prompts  []string
	mu       sync.Mutex
	callIdx  int
	fail     bool
}

func newMockProvider(response string) *mockProvider {
	return &mockProvider{
		name:     "mock",
		model:    "mock-model",
		response: response,
	}
}

func (m *mockProvider) Name() string  { return m.name }
func (m *mockProvider) Model() string { return m.model }

func (m *mockProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	m.mu.Lock()
	m.callIdx++
	m.prompts = append(m.prompts, prompt)
	fail := m.fail
	resp := m.response
	m.mu.Unlock()

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		if fail {
			errCh <- fmt.Errorf("mock: simulated error")
			return
		}

		words := strings.Fields(resp)
		for i, w := range words {
			if i > 0 {
				w = " " + w
			}
			select {
			case tokenCh <- w:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	return tokenCh, errCh
}

func TestRun(t *testing.T) {
	mp := newMockProvider("analysis output here")

	result, err := Run(context.Background(), mp, "log summary", "", nil)
	require.NoError(t, err)

	assert.Equal(t, "analysis output here", result.Analysis)
	assert.Equal(t, "mock-model", result.ModelUsed)
	assert.Equal(t, 1, mp.callIdx)
}

func TestRunStreamCallback(t *testing.T) {
	mp := newMockProvider("hello world")

	var tokens []string
	var mu sync.Mutex

	cb := func(token string) {
		mu.Lock()
		tokens = append(tokens, token)
		mu.Unlock()
	}

	result, err := Run(context.Background(), mp, "summary", "", cb)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, "hello world", strings.Join(tokens, ""))
	assert.Equal(t, "hello world", result.Analysis)
}

func TestRunProviderError(t *testing.T) {
	mp := newMockProvider("")
	mp.fail = true

	result, err := Run(context.Background(), mp, "summary", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analyze:")
	assert.NotNil(t, result)
}

func TestRunDuration(t *testing.T) {
	mp := newMockProvider("output")

	result, err := Run(context.Background(), mp, "summary", "", nil)
	require.NoError(t, err)
	assert.True(t, result.Duration > 0, "Duration should be positive")
}

func TestRunContextCancel(t *testing.T) {
	bp := &blockingProvider{model: "mock-model"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := Run(ctx, bp, "summary", "", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "expected context.Canceled, got: %v", err)
	assert.NotNil(t, result)
}

// blockingProvider sends one token then blocks until context is cancelled.
type blockingProvider struct {
	model string
}

func (p *blockingProvider) Name() string  { return "blocking" }
func (p *blockingProvider) Model() string { return p.model }

func (p *blockingProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		select {
		case tokenCh <- "partial":
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		}

		// Block until cancelled.
		<-ctx.Done()
		errCh <- ctx.Err()
	}()

	return tokenCh, errCh
}

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
	name      string
	model     string
	responses []string // one per call, in order
	prompts   []string // captured prompts from each call
	mu        sync.Mutex
	callIdx   int
	failAt    int // -1 means no failure; otherwise fail on this call index
}

func newMockProvider(responses ...string) *mockProvider {
	return &mockProvider{
		name:      "mock",
		model:     "mock-model",
		responses: responses,
		failAt:    -1,
	}
}

func (m *mockProvider) Name() string  { return m.name }
func (m *mockProvider) Model() string { return m.model }

func (m *mockProvider) Analyze(ctx context.Context, system, prompt string) (<-chan string, <-chan error) {
	tokenCh := make(chan string)
	errCh := make(chan error, 1)

	m.mu.Lock()
	idx := m.callIdx
	m.callIdx++
	m.prompts = append(m.prompts, prompt)
	m.mu.Unlock()

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		if m.failAt == idx {
			errCh <- fmt.Errorf("mock: simulated error on call %d", idx)
			return
		}

		if idx < len(m.responses) {
			// Stream word by word to exercise the channel.
			words := strings.Fields(m.responses[idx])
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
		}
	}()

	return tokenCh, errCh
}

func TestRunMultiPass(t *testing.T) {
	mp := newMockProvider(
		"timeline output",
		"root cause output",
		"recommendations output",
	)

	result, err := Run(context.Background(), mp, "log summary", Options{}, nil)
	require.NoError(t, err)

	assert.Equal(t, "timeline output", result.Timeline)
	assert.Equal(t, "root cause output", result.RootCause)
	assert.Equal(t, "recommendations output", result.Recommendations)
	assert.Equal(t, "mock-model", result.ModelUsed)

	// Verify chaining: pass 2 prompt should contain pass 1 output.
	require.Len(t, mp.prompts, 3)
	assert.Contains(t, mp.prompts[1], "timeline output")
	// Pass 3 prompt should contain both pass 1 and pass 2 output.
	assert.Contains(t, mp.prompts[2], "timeline output")
	assert.Contains(t, mp.prompts[2], "root cause output")
}

func TestRunFastMode(t *testing.T) {
	mp := newMockProvider("combined analysis output")

	result, err := Run(context.Background(), mp, "log summary", Options{Fast: true}, nil)
	require.NoError(t, err)

	// Fast mode stores the single output in all three fields.
	assert.Equal(t, "combined analysis output", result.Timeline)
	assert.Equal(t, "combined analysis output", result.RootCause)
	assert.Equal(t, "combined analysis output", result.Recommendations)

	// Only one call to provider.
	assert.Equal(t, 1, mp.callIdx)
}

func TestRunStreamCallback(t *testing.T) {
	mp := newMockProvider("hello world", "root", "recs")

	type tokenEvent struct {
		pass  Pass
		token string
	}
	var events []tokenEvent
	var mu sync.Mutex

	cb := func(pass Pass, token string) {
		mu.Lock()
		events = append(events, tokenEvent{pass, token})
		mu.Unlock()
	}

	_, err := Run(context.Background(), mp, "summary", Options{}, cb)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	// Verify we got tokens from all three passes.
	passes := map[Pass]bool{}
	for _, ev := range events {
		passes[ev.pass] = true
	}
	assert.True(t, passes[PassTimeline])
	assert.True(t, passes[PassRootCause])
	assert.True(t, passes[PassRecommendations])

	// Verify first pass tokens are in order.
	var timelineTokens []string
	for _, ev := range events {
		if ev.pass == PassTimeline {
			timelineTokens = append(timelineTokens, ev.token)
		}
	}
	assert.Equal(t, "hello world", strings.Join(timelineTokens, ""))
}

func TestRunProviderError(t *testing.T) {
	mp := newMockProvider("timeline output", "", "")
	mp.failAt = 1 // Fail on pass 2 (root cause).

	result, err := Run(context.Background(), mp, "summary", Options{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root cause pass")

	// Pass 1 result should still be available.
	assert.Equal(t, "timeline output", result.Timeline)
}

func TestRunContextCancel(t *testing.T) {
	// blockingProvider blocks mid-stream until context is cancelled.
	bp := &blockingProvider{model: "mock-model"}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay to allow the first pass to start.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := Run(ctx, bp, "summary", Options{}, nil)
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

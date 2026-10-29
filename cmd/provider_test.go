package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProviderTrimsTheKey(t *testing.T) {
	for _, tt := range []struct{ provider, env string }{
		{"openai", "OPENAI_API_KEY"}, {"anthropic", "ANTHROPIC_API_KEY"}, {"gemini", "GEMINI_API_KEY"},
	} {
		t.Run(tt.provider, func(t *testing.T) {
			t.Setenv(tt.env, "  \n")
			_, err := newProvider(tt.provider, "", time.Second)
			require.Error(t, err, "a blank key is not set")
			assert.Contains(t, err.Error(), tt.env+" not set")
		})
	}
}

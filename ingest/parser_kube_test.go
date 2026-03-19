package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestKubeParser(t *testing.T) {
	p := &kubeParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
		wantYear  int
		wantHour  int
	}{
		{
			"stdout full line",
			`2024-01-15T10:00:00.000000000Z stdout F Starting application`,
			true, types.LevelInfo, "Starting application", 2024, 10,
		},
		{
			"stderr error",
			`2024-01-15T10:00:01.000000000Z stderr F ERROR: connection refused`,
			true, types.LevelError, "ERROR: connection refused", 2024, 10,
		},
		{
			"partial line",
			`2024-01-15T10:00:02.000000000Z stdout P partial content`,
			true, types.LevelInfo, "partial content", 2024, 10,
		},
		{
			"prefixed pod info",
			`[pod/checkout-api-68f9d9cdf-vj6fz/checkout-api] 2026-03-19 12:29:30,963 INFO CHECKOUT_API APP_LOG some message`,
			true, types.LevelInfo, "INFO CHECKOUT_API APP_LOG some message", 2026, 12,
		},
		{
			"prefixed pod warn",
			`[pod/checkout-api-68f9d9cdf-sbtjc/checkout-api] 2026-03-19 12:29:22,680 WARN CHECKOUT_API APP_LOG some warning`,
			true, types.LevelWarn, "WARN CHECKOUT_API APP_LOG some warning", 2026, 12,
		},
		{
			"not kube format",
			`just some text`,
			false, types.LevelUnknown, "", 0, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantMsg, entry.Message)
				assert.Equal(t, tt.wantYear, entry.Timestamp.Year())
				assert.Equal(t, tt.wantHour, entry.Timestamp.Hour())
			}
		})
	}
}

func TestKubeParserPrefixedMetadata(t *testing.T) {
	p := &kubeParser{}
	entry, ok := p.Parse(
		`[pod/checkout-api-68f9d9cdf-vj6fz/checkout-api] 2026-03-19 12:29:30,963 INFO some message`,
		1, "test.log",
	)
	assert.True(t, ok)
	assert.Equal(t, "pod/checkout-api-68f9d9cdf-vj6fz/checkout-api", entry.Metadata["resource"])
}

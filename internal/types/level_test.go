package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"TRACE", LevelTrace},
		{"DEBUG", LevelDebug},
		{"INFO", LevelInfo},
		{"WARN", LevelWarn},
		{"WARNING", LevelWarn},
		{"ERROR", LevelError},
		{"ERR", LevelError},
		{"FATAL", LevelFatal},
		{"CRITICAL", LevelFatal},
		{"CRIT", LevelFatal},
		{"trace", LevelTrace},
		{"debug", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"err", LevelError},
		{"fatal", LevelFatal},
		{"T", LevelTrace},
		{"D", LevelDebug},
		{"I", LevelInfo},
		{"W", LevelWarn},
		{"E", LevelError},
		{"F", LevelFatal},
		{"0", LevelFatal},
		{"1", LevelFatal},
		{"2", LevelFatal},
		{"3", LevelError},
		{"4", LevelWarn},
		{"5", LevelWarn},
		{"6", LevelInfo},
		{"7", LevelDebug},
		{"", LevelUnknown},
		{"garbage", LevelUnknown},
		{"123", LevelUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseLevel(tt.input))
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelUnknown, "UNKNOWN"},
		{LevelTrace, "TRACE"},
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelFatal, "FATAL"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
		})
	}
}

func TestLevelMeets(t *testing.T) {
	tests := []struct {
		level     Level
		threshold Level
		expected  bool
	}{
		{LevelError, LevelWarn, true},
		{LevelWarn, LevelWarn, true},
		{LevelInfo, LevelWarn, false},
		{LevelFatal, LevelTrace, true},
		{LevelUnknown, LevelTrace, false},
	}
	for _, tt := range tests {
		t.Run(tt.level.String()+">="+tt.threshold.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.Meets(tt.threshold))
		})
	}
}

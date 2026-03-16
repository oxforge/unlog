package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "UUID replacement",
			input:    "Request abc12345-1234-5678-9abc-def012345678 failed",
			expected: "Request <UUID> failed",
		},
		{
			name:     "ISO timestamp replacement",
			input:    "Event at 2024-01-15T10:30:45 was logged",
			expected: "Event at <TS> was logged",
		},
		{
			name:     "ISO timestamp with space separator",
			input:    "Event at 2024-01-15 10:30:45 was logged",
			expected: "Event at <TS> was logged",
		},
		{
			name:     "IPv4 replacement",
			input:    "Connection to 10.0.1.42 timed out",
			expected: "Connection to <IP> timed out",
		},
		{
			name:     "IPv4 with port replacement",
			input:    "Connection to 10.0.1.42:5432 timed out",
			expected: "Connection to <IP>:<NUM> timed out",
		},
		{
			name:     "hex string replacement 8+ chars",
			input:    "Commit abcdef0123456789 deployed",
			expected: "Commit <HEX> deployed",
		},
		{
			name:     "file path replacement",
			input:    "Error reading /var/log/app/server.log file",
			expected: "Error reading <PATH> file",
		},
		{
			name:     "quoted string replacement",
			input:    `Failed to process message "hello world" in queue`,
			expected: "Failed to process message <STR> in queue",
		},
		{
			name:     "standalone number replacement",
			input:    "Request took 1247 ms with 3 retries",
			expected: "Request took <NUM> ms with <NUM> retries",
		},
		{
			name:     "combined replacements",
			input:    "Connection to 10.0.1.42:5432 timed out after 30s for request abc12345-1234-5678-9abc-def012345678",
			expected: "Connection to <IP>:<NUM> timed out after <NUM>s for request <UUID>",
		},
		{
			name:     "no replacements needed",
			input:    "Server started successfully",
			expected: "Server started successfully",
		},
		{
			name:     "IPv6 replacement",
			input:    "Listening on 2001:0db8:85a3:0000:0000:8a2e:0370:7334 port 80",
			expected: "Listening on <IP> port <NUM>",
		},
		{
			name:     "similar messages produce same signature",
			input:    "Connection to 192.168.1.1:3306 timed out after 10s",
			expected: "Connection to <IP>:<NUM> timed out after <NUM>s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSignature(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}

	// Cross-test: semantically similar messages produce the same signature
	t.Run("similar messages same signature", func(t *testing.T) {
		sig1 := ExtractSignature("Connection to 10.0.1.42:5432 timed out after 30s")
		sig2 := ExtractSignature("Connection to 192.168.1.1:3306 timed out after 10s")
		assert.Equal(t, sig1, sig2)
	})
}

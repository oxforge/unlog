package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestDockerComposeParser(t *testing.T) {
	p := &dockerComposeParser{}
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
			"info with timestamp",
			`web-1     | 2024-01-15 10:00:00 INFO Starting application`,
			true, types.LevelInfo, "INFO Starting application", 2024, 10,
		},
		{
			"error with timestamp millis",
			`web-1     | 2024-01-15 10:00:01.500 ERROR Connection failed`,
			true, types.LevelError, "ERROR Connection failed", 2024, 10,
		},
		{
			"warn with comma millis (Java-style)",
			`web-1     | 2024-01-15 10:00:02,300 WARN High memory usage`,
			true, types.LevelWarn, "WARN High memory usage", 2024, 10,
		},
		{
			"no timestamp",
			`redis-1   | Ready to accept connections`,
			true, types.LevelUnknown, "Ready to accept connections", 0, 0,
		},
		{
			"stack trace line",
			`web-1     |     at com.example.Main.run(Main.java:42)`,
			true, types.LevelUnknown, "    at com.example.Main.run(Main.java:42)", 0, 0,
		},
		{
			"not compose format",
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
				if tt.wantYear > 0 {
					assert.Equal(t, tt.wantYear, entry.Timestamp.Year())
					assert.Equal(t, tt.wantHour, entry.Timestamp.Hour())
				}
			}
		})
	}
}

func TestDockerComposeParserMetadata(t *testing.T) {
	p := &dockerComposeParser{}

	entry, ok := p.Parse(`web-1     | 2024-01-15 10:00:00 INFO test`, 1, "test.log")
	assert.True(t, ok)
	assert.Equal(t, "web-1", entry.Metadata["container"])

	entry, ok = p.Parse(`my.service-name-1 | some message`, 1, "test.log")
	assert.True(t, ok)
	assert.Equal(t, "my.service-name-1", entry.Metadata["container"])
}

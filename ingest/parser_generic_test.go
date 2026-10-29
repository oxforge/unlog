package ingest

import (
	"testing"

	"github.com/oxforge/unlog/types"
	"github.com/stretchr/testify/assert"
)

func TestGenericParser(t *testing.T) {
	p := &genericParser{}
	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantLevel types.Level
		wantMsg   string
	}{
		{
			"ISO timestamp with level",
			"2024-01-15 10:00:00 ERROR connection refused",
			true, types.LevelError, "ERROR connection refused",
		},
		{
			"ISO T-separated",
			"2024-01-15T10:00:00 INFO starting server",
			true, types.LevelInfo, "INFO starting server",
		},
		{
			"no level keyword",
			"2024-01-15 10:00:00 something happened",
			true, types.LevelUnknown, "something happened",
		},
		{
			"no timestamp",
			"just some text",
			false, types.LevelUnknown, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := p.Parse(tt.line, 1, "test.log")
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantLevel, entry.Level)
				assert.Equal(t, tt.wantMsg, entry.Message)
				// Timestamp assertions
				assert.Equal(t, 2024, entry.Timestamp.Year())
				assert.Equal(t, 10, entry.Timestamp.Hour())
			}
		})
	}
}

func TestGenericParserTrailingPairs(t *testing.T) {
	p := &genericParser{}
	entry, ok := p.Parse(`2024-01-15 10:00:01 ERROR payment failed user=42 amount=10 currency=USD`, 1, "app.log")
	assert.True(t, ok)
	assert.Equal(t, types.LevelError, entry.Level)
	assert.Equal(t, "ERROR payment failed user=42 amount=10 currency=USD", entry.Message)
	assert.Equal(t, map[string]string{"user": "42", "amount": "10", "currency": "USD"}, entry.Metadata)
}

func TestGenericParserLogfmtAfterTimestamp(t *testing.T) {
	p := &genericParser{}
	entry, ok := p.Parse(`2024-01-15T10:00:01Z level=error msg="disk full" path=/data`, 1, "app.log")
	assert.True(t, ok)
	assert.Equal(t, types.LevelError, entry.Level)
	assert.Equal(t, "disk full", entry.Message)
	assert.Equal(t, map[string]string{"path": "/data"}, entry.Metadata)
}

func TestGenericParserEqualsInsideFreeText(t *testing.T) {
	p := &genericParser{}
	entry, ok := p.Parse(`2024-01-15 10:00:01 WARN expected a == b in check`, 1, "app.log")
	assert.True(t, ok)
	assert.Equal(t, "WARN expected a == b in check", entry.Message)
	assert.Empty(t, entry.Metadata)
}

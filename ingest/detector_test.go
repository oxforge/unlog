package ingest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected Format
	}{
		{
			"docker json",
			[]string{
				`{"log":"Starting server\n","stream":"stdout","time":"2024-01-15T10:00:00.000000000Z"}`,
				`{"log":"Listening on port 8080\n","stream":"stdout","time":"2024-01-15T10:00:01.000000000Z"}`,
			},
			FormatDockerJSON,
		},
		{
			"cloudwatch",
			[]string{
				`{"@timestamp":"2024-01-15T10:00:00.000Z","@message":"Starting up","@logStream":"app/prod"}`,
				`{"@timestamp":"2024-01-15T10:00:01.000Z","@message":"Ready","@logStream":"app/prod"}`,
			},
			FormatCloudWatch,
		},
		{
			"kubernetes",
			[]string{
				`2024-01-15T10:00:00.000000000Z stdout F Starting application`,
				`2024-01-15T10:00:01.000000000Z stderr F Error connecting to DB`,
			},
			FormatKubernetes,
		},
		{
			"json structured",
			[]string{
				`{"level":"info","msg":"starting","ts":"2024-01-15T10:00:00Z"}`,
				`{"level":"error","msg":"failed","ts":"2024-01-15T10:00:01Z"}`,
			},
			FormatJSON,
		},
		{
			"syslog rfc5424",
			[]string{
				`<165>1 2024-01-15T10:00:00.000Z myhost myapp 1234 - - Starting`,
				`<165>1 2024-01-15T10:00:01.000Z myhost myapp 1234 - - Ready`,
			},
			FormatSyslog5424,
		},
		{
			"syslog rfc3164",
			[]string{
				`<34>Jan 15 10:00:00 myhost myapp[1234]: Starting`,
				`<34>Jan 15 10:00:01 myhost myapp[1234]: Ready`,
			},
			FormatSyslog3164,
		},
		{
			"apache clf",
			[]string{
				`192.168.1.1 - - [15/Jan/2024:10:00:00 -0700] "GET /api/users HTTP/1.1" 200 1234`,
				`192.168.1.2 - - [15/Jan/2024:10:00:01 -0700] "POST /api/login HTTP/1.1" 401 56`,
			},
			FormatCLF,
		},
		{
			"logfmt",
			[]string{
				`ts=2024-01-15T10:00:00Z level=info msg="starting server" port=8080`,
				`ts=2024-01-15T10:00:01Z level=error msg="connection failed" host=db`,
			},
			FormatLogfmt,
		},
		{
			"generic timestamp",
			[]string{
				`2024-01-15 10:00:00 INFO Starting application`,
				`2024-01-15 10:00:01 ERROR Connection failed`,
			},
			FormatGeneric,
		},
		{
			"raw fallback",
			[]string{
				`some random log line`,
				`another random line without timestamps`,
			},
			FormatRaw,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := detectFormat(tt.lines)
			assert.Equal(t, tt.expected, format)
		})
	}
}

func TestDetectFormatMajorityVote(t *testing.T) {
	lines := []string{
		`{"level":"info","msg":"a","ts":"2024-01-15T10:00:00Z"}`,
		`{"level":"info","msg":"b","ts":"2024-01-15T10:00:01Z"}`,
		`{"level":"info","msg":"c","ts":"2024-01-15T10:00:02Z"}`,
		`{"level":"info","msg":"d","ts":"2024-01-15T10:00:03Z"}`,
		`{"level":"info","msg":"e","ts":"2024-01-15T10:00:04Z"}`,
		`{"level":"info","msg":"f","ts":"2024-01-15T10:00:05Z"}`,
		`{"level":"info","msg":"g","ts":"2024-01-15T10:00:06Z"}`,
		`garbage line 1`,
		`garbage line 2`,
		`garbage line 3`,
	}
	assert.Equal(t, FormatJSON, detectFormat(lines))
}

func TestDetectFormatBelowThreshold(t *testing.T) {
	lines := []string{
		`{"level":"info","msg":"a","ts":"2024-01-15T10:00:00Z"}`,
		`garbage line 1`,
		`{"level":"info","msg":"b","ts":"2024-01-15T10:00:01Z"}`,
		`garbage line 2`,
	}
	format := detectFormat(lines)
	assert.True(t, format == FormatGeneric || format == FormatRaw)
}

package ingest

import (
	"strings"
	"testing"
)

// Sample lines from testdata/formats/ — realistic production log lines.
var (
	sampleJSON       = `{"level":"error","msg":"Connection to database failed","ts":"2024-01-15T10:00:10Z","service":"api","host":"db-primary"}`
	sampleLogfmt     = `ts=2024-01-15T10:00:10Z level=error msg="Connection pool exhausted" service=api active=100 max=100`
	sampleSyslog3164 = `<131>Jan 15 10:00:11 app-1 myapp[9012]: ERROR: unable to acquire connection`
	sampleSyslog5424 = `<165>1 2024-01-15T10:00:00.000Z web-1 nginx 1234 - - Starting worker process`
	sampleCLF        = `10.0.0.1 - - [15/Jan/2024:10:00:05 -0700] "GET /api/orders HTTP/1.1" 500 789`
	sampleGenericTS  = `2024-01-15 10:00:10 ERROR Connection refused: db-primary:5432`

	sampleMixed = []string{
		sampleJSON,
		sampleLogfmt,
		sampleSyslog3164,
		sampleCLF,
		sampleGenericTS,
		sampleJSON,
		sampleJSON,
		sampleJSON,
		sampleJSON,
		sampleJSON,
	}

	sampleStackTrace = "2024-01-15 10:00:10 ERROR Exception in thread \"main\"\n" +
		"java.lang.NullPointerException\n" +
		"\tat com.example.App.process(App.java:42)\n" +
		"\tat com.example.App.handle(App.java:28)\n" +
		"\tat com.example.App.main(App.java:10)"
)

func BenchmarkFormatDetection(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DetectFormat(sampleMixed)
	}
}

func BenchmarkParseJSON(b *testing.B) {
	p := &jsonParser{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(sampleJSON, 1, "test.log")
	}
}

func BenchmarkParseLogfmt(b *testing.B) {
	p := &logfmtParser{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(sampleLogfmt, 1, "test.log")
	}
}

func BenchmarkParseSyslog3164(b *testing.B) {
	p := newSyslogParser(false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(sampleSyslog3164, 1, "test.log")
	}
}

func BenchmarkParseSyslog5424(b *testing.B) {
	p := newSyslogParser(true)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(sampleSyslog5424, 1, "test.log")
	}
}

func BenchmarkParseCLF(b *testing.B) {
	p := &clfParser{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(sampleCLF, 1, "test.log")
	}
}

func BenchmarkParseGenericTimestamp(b *testing.B) {
	p := &genericParser{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Parse(sampleGenericTS, 1, "test.log")
	}
}

func BenchmarkTimestampParse(b *testing.B) {
	ts := "2024-01-15T10:00:10Z"

	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			c := &formatCache{}
			c.Parse(ts)
		}
	})

	b.Run("warm", func(b *testing.B) {
		c := &formatCache{}
		c.Parse(ts) // warm up the cache
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			c.Parse(ts)
		}
	})
}

func BenchmarkMultiLineReassembly(b *testing.B) {
	checker := lineCheckerForFormat(FormatGeneric)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rd := newReader(strings.NewReader(sampleStackTrace), "test.log", checker)
		_, _ = rd.ReadAll()
	}
}

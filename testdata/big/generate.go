//go:build ignore

package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	sizeFlag      = flag.String("size", "100MB", "Target file size (e.g., 10MB, 100MB, 1GB)")
	formatFlag    = flag.String("format", "json", "Log format: json, logfmt, syslog, clf, generic")
	errorRateFlag = flag.Float64("error-rate", 0.05, "Fraction of entries that are ERROR/FATAL (0.0-1.0)")
	outputFlag    = flag.String("output", "", "Output file path (default: stdout)")
)

var services = []string{"api-gateway", "user-service", "payment-service", "order-service", "notification-service", "db-proxy"}

// Chain-triggering error sequences for realism.
var chainErrors = [][]string{
	// DB connection exhaustion
	{"deadlock detected on table orders", "connection pool exhausted, 0 available connections", "query failed: cannot execute SELECT on orders"},
	// OOM cascade
	{"memory warning: heap usage exceeds 90%", "OOM killed process 4521", "pod restart: payment-service-abc123"},
	// Deployment failure
	{"deploying version v2.3.1", "health check failed: /healthz returned 503", "rollback initiated for version v2.3.1"},
}

var warnMessages = []string{
	"request latency exceeds threshold: 2500ms",
	"connection pool usage at 85%",
	"disk space warning: /data at 88%",
	"retry attempt 3/5 for upstream call",
	"TLS certificate expires in 7 days",
	"consumer lag detected: 15000 messages behind",
	"rate limit approaching: 450/500 requests per second",
}

var infoMessages = []string{
	"GET /api/users 200 OK 45ms",
	"POST /api/orders 201 Created 120ms",
	"processed batch of 500 events",
	"cache hit ratio: 94.2%",
	"health check passed",
	"connection established to redis-primary:6379",
	"starting application on port 8080",
}

func main() {
	flag.Parse()

	targetBytes, err := parseSize(*sizeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// For .tar.gz/.tgz, generate to a temp file first, then pack it.
	// For .gz and plain files, stream directly.
	outputPath := *outputFlag
	needsTar := isTarGz(outputPath)
	if needsTar {
		// Write plain log to a temp file, pack into tar.gz afterward.
		tmp, err := os.CreateTemp("", "unlog-gen-*.log")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating temp file: %v\n", err)
			os.Exit(1)
		}
		defer os.Remove(tmp.Name())
		outputPath = tmp.Name()
		tmp.Close()
	}

	out, cleanup, err := createWriter(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating output: %v\n", err)
		os.Exit(1)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ts := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	var written int64
	lineNum := 0
	totalLines := int(targetBytes / 150) // rough estimate for progress

	// Incident spike zone: middle 20% of the file.
	spikeStart := totalLines * 4 / 10
	spikeEnd := totalLines * 6 / 10

	chainIdx := 0
	chainStep := 0
	inChain := false

	for written < targetBytes {
		lineNum++
		ts = ts.Add(time.Duration(rng.Intn(100)+10) * time.Millisecond)

		svc := services[rng.Intn(len(services))]
		isSpike := lineNum >= spikeStart && lineNum <= spikeEnd

		var level, msg string

		// During spike, inject chain error sequences periodically.
		if isSpike && !inChain && rng.Float64() < 0.01 {
			inChain = true
			chainIdx = rng.Intn(len(chainErrors))
			chainStep = 0
		}

		if inChain {
			level = "ERROR"
			if chainStep == 0 {
				level = "WARN"
			}
			msg = chainErrors[chainIdx][chainStep]
			chainStep++
			if chainStep >= len(chainErrors[chainIdx]) {
				inChain = false
			}
		} else if rng.Float64() < *errorRateFlag && isSpike {
			// Higher error rate during spike.
			if rng.Float64() < 0.3 {
				level = "FATAL"
			} else {
				level = "ERROR"
			}
			msg = fmt.Sprintf("Error: request processing failed with status=%d trace_id=%s",
				[]int{500, 502, 503}[rng.Intn(3)],
				randomHex(rng, 16))
		} else if rng.Float64() < *errorRateFlag*0.3 {
			level = "WARN"
			msg = warnMessages[rng.Intn(len(warnMessages))]
		} else {
			level = "INFO"
			msg = infoMessages[rng.Intn(len(infoMessages))]
		}

		line := formatLine(*formatFlag, ts, level, svc, msg)
		n, _ := fmt.Fprintln(out, line)
		written += int64(n)
	}

	cleanup()

	if needsTar {
		if err := packTarGz(outputPath, *outputFlag); err != nil {
			fmt.Fprintf(os.Stderr, "error creating tar.gz: %v\n", err)
			os.Exit(1)
		}
	}

	if *outputFlag != "" && *outputFlag != "-" {
		fi, _ := os.Stat(*outputFlag)
		compressedSize := written
		if fi != nil {
			compressedSize = fi.Size()
		}
		fmt.Fprintf(os.Stderr, "Generated %s (%d lines) to %s [%s on disk]\n",
			formatBytes(written), lineNum, *outputFlag, formatBytes(compressedSize))
	}
}

func isTarGz(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
}

// createWriter returns an io.Writer and a cleanup function.
// For .gz files, wraps in gzip. For plain files and stdout, writes directly.
func createWriter(output string) (io.Writer, func(), error) {
	if output == "" || output == "-" {
		return os.Stdout, func() {}, nil
	}

	f, err := os.Create(output)
	if err != nil {
		return nil, nil, err
	}

	if strings.HasSuffix(strings.ToLower(output), ".gz") {
		gw := gzip.NewWriter(f)
		return gw, func() { gw.Close(); f.Close() }, nil
	}

	return f, func() { f.Close() }, nil
}

// packTarGz reads src (a plain file) and writes it as a single entry into a .tar.gz at dst.
func packTarGz(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	outFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Derive inner filename from dst: test.tar.gz → test.log
	inner := filepath.Base(dst)
	inner = strings.TrimSuffix(inner, ".tar.gz")
	inner = strings.TrimSuffix(inner, ".tgz")
	inner += ".log"

	hdr := &tar.Header{
		Name:    inner,
		Mode:    0644,
		Size:    info.Size(),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	_, err = io.Copy(tw, in)
	return err
}

func formatLine(format string, ts time.Time, level, source, msg string) string {
	switch format {
	case "json":
		return fmt.Sprintf(`{"timestamp":"%s","level":"%s","service":"%s","message":"%s"}`,
			ts.Format(time.RFC3339Nano), level, source, escapeJSON(msg))
	case "logfmt":
		return fmt.Sprintf(`ts=%s level=%s service=%s msg="%s"`,
			ts.Format(time.RFC3339Nano), strings.ToLower(level), source, msg)
	case "syslog":
		return fmt.Sprintf(`<%d>1 %s %s app - - - %s`,
			syslogPriority(level), ts.Format(time.RFC3339), source, msg)
	case "clf":
		return fmt.Sprintf(`%s - - [%s] "GET /api/data HTTP/1.1" %s 1234`,
			source, ts.Format("02/Jan/2006:15:04:05 -0700"), clfStatus(level))
	default: // generic
		return fmt.Sprintf(`%s %s [%s] %s`,
			ts.Format("2006-01-02 15:04:05.000"), source, level, msg)
	}
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func syslogPriority(level string) int {
	switch level {
	case "FATAL":
		return 8*1 + 2 // user.crit
	case "ERROR":
		return 8*1 + 3 // user.err
	case "WARN":
		return 8*1 + 4 // user.warning
	default:
		return 8*1 + 6 // user.info
	}
}

func clfStatus(level string) string {
	switch level {
	case "FATAL", "ERROR":
		return "500"
	case "WARN":
		return "400"
	default:
		return "200"
	}
}

func randomHex(rng *rand.Rand, length int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, length)
	for i := range b {
		b[i] = hexChars[rng.Intn(len(hexChars))]
	}
	return string(b)
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	// Ordered longest-suffix-first to avoid "B" matching before "GB"/"MB"/"KB".
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}
	for _, sm := range suffixes {
		if strings.HasSuffix(s, sm.suffix) {
			numStr := strings.TrimSuffix(s, sm.suffix)
			var num float64
			if _, err := fmt.Sscanf(numStr, "%f", &num); err != nil {
				return 0, fmt.Errorf("cannot parse size %q", s)
			}
			if num <= 0 {
				return 0, fmt.Errorf("size must be positive, got %q", s)
			}
			return int64(num * float64(sm.mult)), nil
		}
	}
	return 0, fmt.Errorf("cannot parse size %q (use B, KB, MB, GB suffix)", s)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.1fGB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

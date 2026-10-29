package analyze

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// sseEvent represents a single Server-Sent Event.
type sseEvent struct {
	Event string // event type (empty if not set)
	Data  string // data payload
}

// parseSSE reads SSE events from r and sends them on the returned channel.
// Once the channel is closed, the returned func says why reading stopped:
// nil at the end of the stream, else the read error or ctx's error.
func parseSSE(ctx context.Context, r io.Reader) (<-chan sseEvent, func() error) {
	ch := make(chan sseEvent, 8)
	var stopErr error
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		var event sseEvent
		var dataLines []string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				stopErr = ctx.Err()
				return
			default:
			}

			line := scanner.Text()

			if line == "" {
				if len(dataLines) > 0 {
					event.Data = strings.Join(dataLines, "\n")
					select {
					case ch <- event:
					case <-ctx.Done():
						stopErr = ctx.Err()
						return
					}
				}
				event = sseEvent{}
				dataLines = nil
				continue
			}

			if strings.HasPrefix(line, ":") {
				continue
			}

			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")

			switch field {
			case "event":
				event.Event = value
			case "data":
				dataLines = append(dataLines, value)
			}
		}

		if stopErr = scanner.Err(); stopErr == nil {
			stopErr = ctx.Err()
		}
		if len(dataLines) > 0 && stopErr == nil {
			event.Data = strings.Join(dataLines, "\n")
			select {
			case ch <- event:
			case <-ctx.Done():
				stopErr = ctx.Err()
			}
		}
	}()
	return ch, func() error { return stopErr }
}

// errStreamEndedEarly is the error of a stream that ended before the
// provider's end-of-stream event: a dropped connection or a proxy timeout.
var errStreamEndedEarly = errors.New("stream ended early")

// streamEnd is the error for a stream whose events ran out before its end
// event: why reading stopped, or errStreamEndedEarly.
func streamEnd(stopped func() error) error {
	if err := stopped(); err != nil {
		return err
	}
	return errStreamEndedEarly
}

package analyze

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// sseEvent represents a single Server-Sent Event.
type sseEvent struct {
	Event string // event type (empty if not set)
	Data  string // data payload
}

// parseSSE reads SSE events from r and sends them on the returned channel.
func parseSSE(ctx context.Context, r io.Reader) <-chan sseEvent {
	ch := make(chan sseEvent, 8)
	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(r)
		var event sseEvent
		var dataLines []string

		for scanner.Scan() {
			select {
			case <-ctx.Done():
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

		if len(dataLines) > 0 {
			event.Data = strings.Join(dataLines, "\n")
			select {
			case ch <- event:
			case <-ctx.Done():
			}
		}
	}()
	return ch
}

package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxEventBytes bounds one line of the stream. The API caps a log line at 1 MiB,
// so this leaves room for the field name around it.
const maxEventBytes = 2 << 20

// ErrStop ends a stream early without it being an error: the handler has seen
// what it wanted.
var ErrStop = errors.New("stop the stream")

// stream sends r and hands each server-sent event to fn until the stream ends,
// ctx is done, or fn returns an error. A handler returning ErrStop ends it
// cleanly.
func (c *Client) stream(ctx context.Context, r request, fn func(Event) error) error {

	r.accept = "text/event-stream"
	resp, err := c.do(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	err = readEvents(resp.Body, fn)
	if errors.Is(err, ErrStop) || ctx.Err() != nil {
		return nil
	}
	return err
}

// readEvents parses the text/event-stream format: `event:` and `data:` fields,
// one event per blank line, comments starting with a colon.
func readEvents(body io.Reader, fn func(Event) error) error {

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), maxEventBytes)

	var name string
	var data []string
	dispatch := func() error {

		if name == "" && data == nil {
			return nil
		}
		ev := Event{Name: name, Data: strings.Join(data, "\n")}
		if ev.Name == "" {
			ev.Name = "message"
		}
		name, data = "", nil
		return fn(ev)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			name = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read the event stream: %w", err)
	}
	//a stream cut off mid-event still delivers what it had.
	return dispatch()
}

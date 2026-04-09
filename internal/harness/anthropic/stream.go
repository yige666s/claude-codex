package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
)

func (c *Client) StreamMessages(ctx context.Context, request MessageRequest) (<-chan StreamEvent, <-chan error) {
	events := make(chan StreamEvent)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		err := withRetry(ctx, c.retryConfig, func() error {
			request.Stream = true
			body, err := json.Marshal(request)
			if err != nil {
				return err
			}

			httpRequest, err := c.newRequest(ctx, bytes.NewReader(body))
			if err != nil {
				return err
			}

			response, err := c.httpClient.Do(httpRequest)
			if err != nil {
				return err
			}
			defer response.Body.Close()

			if response.StatusCode >= 400 {
				data, _ := io.ReadAll(response.Body)
				return &HTTPError{
					StatusCode: response.StatusCode,
					Status:     response.Status,
					Body:       string(data),
				}
			}

			scanner := bufio.NewScanner(response.Body)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

			var eventName string
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					eventName = ""
					continue
				}

				if len(line) > 6 && line[:6] == "event:" {
					eventName = line[6:]
					eventName = trimSSEValue(eventName)
					continue
				}

				if len(line) > 5 && line[:5] == "data:" {
					data := trimSSEValue(line[5:])
					if data == "[DONE]" {
						return nil
					}

					events <- StreamEvent{
						Event: eventName,
						Data:  json.RawMessage(data),
					}
				}
			}

			if err := scanner.Err(); err != nil && err != io.EOF {
				return err
			}

			return nil
		})

		if err != nil {
			errs <- err
		}
	}()

	return events, errs
}

func trimSSEValue(value string) string {
	for len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return value
}

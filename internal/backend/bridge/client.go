package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

type Client struct {
	secret  string
	encoder *json.Encoder
	decoder *json.Decoder
	nextID  int64
	mu      sync.Mutex
}

func NewClient(secret string, in io.Reader, out io.Writer) *Client {
	return &Client{
		secret:  secret,
		encoder: json.NewEncoder(out),
		decoder: json.NewDecoder(in),
	}
}

func (c *Client) RunPrompt(ctx context.Context, workingDir, prompt string) (*RunPromptResult, error) {
	var result RunPromptResult
	if err := c.call(ctx, Request{
		Method:     MethodRunPrompt,
		WorkingDir: workingDir,
		Prompt:     prompt,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListTools(ctx context.Context, workingDir string) (*ListToolsResult, error) {
	var result ListToolsResult
	if err := c.call(ctx, Request{
		Method:     MethodListTools,
		WorkingDir: workingDir,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) CreateSession(ctx context.Context, workingDir string) (*CreateSessionResult, error) {
	var result CreateSessionResult
	if err := c.call(ctx, Request{
		Method:     MethodCreateSession,
		WorkingDir: workingDir,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) RunSessionPrompt(ctx context.Context, sessionID, prompt string) (*SessionPromptResult, error) {
	var result SessionPromptResult
	if err := c.call(ctx, Request{
		Method:    MethodSessionPrompt,
		SessionID: sessionID,
		Prompt:    prompt,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetSession(ctx context.Context, sessionID string) (*GetSessionResult, error) {
	var result GetSessionResult
	if err := c.call(ctx, Request{
		Method:    MethodGetSession,
		SessionID: sessionID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListSessions(ctx context.Context, workingDir string) (*ListSessionsResult, error) {
	var result ListSessionsResult
	if err := c.call(ctx, Request{
		Method:     MethodListSessions,
		WorkingDir: workingDir,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) DeleteSession(ctx context.Context, sessionID string) (*DeleteSessionResult, error) {
	var result DeleteSessionResult
	if err := c.call(ctx, Request{
		Method:    MethodDeleteSession,
		SessionID: sessionID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) call(ctx context.Context, request Request, target any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	request.ID = atomic.AddInt64(&c.nextID, 1)
	request.Secret = c.secret

	if err := c.encoder.Encode(request); err != nil {
		return err
	}

	var response Response
	if err := c.decoder.Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	if target == nil || len(response.Result) == 0 {
		return nil
	}
	return json.Unmarshal(response.Result, target)
}

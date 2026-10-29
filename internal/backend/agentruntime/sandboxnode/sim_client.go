package sandboxnode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func (c *ProbeClient) simulate(ctx context.Context, method, path string, body any, out any) error {
	if c == nil {
		return ErrLeaseMissing
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	endpoint := *c.base
	parsed, err := url.Parse(path)
	if err != nil {
		return err
	}
	endpoint.Path = parsed.Path
	endpoint.RawQuery = parsed.RawQuery
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	signer, err := NewTaskSigner([]byte(c.token), nil)
	if err != nil {
		return err
	}
	if err = signer.Sign(req, data); err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sandbox simulation returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(out)
}
func (c *ProbeClient) SimulatePrepare(ctx context.Context, spec PrepareSpec) (LeaseSnapshot, error) {
	var lease LeaseSnapshot
	err := c.simulate(ctx, http.MethodPost, "/v1/sim/prepare", spec, &lease)
	return lease, err
}
func (c *ProbeClient) SimulateEvents(ctx context.Context, id, user string, cursor int64) ([]LeaseEvent, int64, error) {
	q := url.Values{"lease_id": {id}, "user_id": {user}, "cursor": {strconv.FormatInt(cursor, 10)}}
	var result struct {
		Events     []LeaseEvent `json:"events"`
		NextCursor int64        `json:"next_cursor"`
	}
	err := c.simulate(ctx, http.MethodGet, "/v1/sim/events?"+q.Encode(), nil, &result)
	return result.Events, result.NextCursor, err
}
func (c *ProbeClient) SimulateCancel(ctx context.Context, id, user string) (LeaseSnapshot, error) {
	var lease LeaseSnapshot
	err := c.simulate(ctx, http.MethodPost, "/v1/sim/cancel", map[string]string{"lease_id": id, "user_id": user}, &lease)
	return lease, err
}
func (c *ProbeClient) SimulateDispose(ctx context.Context, id, user string) (LeaseSnapshot, error) {
	var lease LeaseSnapshot
	err := c.simulate(ctx, http.MethodPost, "/v1/sim/dispose", map[string]string{"lease_id": id, "user_id": user}, &lease)
	return lease, err
}

type SimulatedExecution struct {
	ExecutionID  string `json:"execution_id"`
	Status       string `json:"status"`
	CodeExecuted bool   `json:"code_executed"`
}

func (c *ProbeClient) SimulateExec(ctx context.Context, id, user string) (SimulatedExecution, error) {
	var v SimulatedExecution
	err := c.simulate(ctx, http.MethodPost, "/v1/sim/exec", map[string]string{"lease_id": id, "user_id": user}, &v)
	return v, err
}
func (c *ProbeClient) SimulateCollect(ctx context.Context, id, user string) (bool, error) {
	var v struct {
		SimulationOnly bool     `json:"simulation_only"`
		Artifacts      []string `json:"artifacts"`
	}
	err := c.simulate(ctx, http.MethodPost, "/v1/sim/collect", map[string]string{"lease_id": id, "user_id": user}, &v)
	return v.SimulationOnly && len(v.Artifacts) == 0, err
}

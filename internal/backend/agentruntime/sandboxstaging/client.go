package sandboxstaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxoci"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

// Client only stages and disposes input. No execute method exists here.
// Real execution requires a separate audited protocol and Node driver.
type Client struct {
	base   *url.URL
	http   *http.Client
	signer *sandboxnode.TaskSigner
}

const nodeRequestTimeout = 60 * time.Second
const nodeExecuteTimeout = 61 * time.Minute // Profile execution is bounded to 3600 seconds.

func NewClient(endpoint string, key []byte, transport *http.Client) (*Client, error) {
	base, err := url.Parse(endpoint)
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid node staging endpoint")
	}
	ip := net.ParseIP(base.Hostname())
	if base.Scheme != "https" && !(base.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, errors.New("staging endpoint requires HTTPS or loopback")
	}
	signer, err := sandboxnode.NewTaskSigner(key, nil)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		transport = &http.Client{}
	}
	copyClient := *transport
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, http: &copyClient, signer: signer}, nil
}
func (c *Client) send(ctx context.Context, path string, body any, out any) error {
	if c == nil || ctx == nil {
		return errors.New("staging client unavailable")
	}
	timeout := nodeRequestTimeout
	if path == "/v1/dev/execute" {
		timeout = nodeExecuteTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if len(data) > MaxStagingBody {
		return errors.New("staging request too large")
	}
	endpoint := *c.base
	endpoint.Path = path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if err := c.signer.Sign(request, data); err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("node staging returned HTTP %d", response.StatusCode)
	}
	// Prepare/dispose have tiny acknowledgements. Execution and collection
	// legitimately carry bounded output bytes (base64 expands binary artifacts).
	// Node applies its own response cap; the client enforces the same cap and
	// rejects trailing JSON rather than silently decoding a truncated stream.
	maxResponse := int64(16 << 10)
	if path == "/v1/dev/execute" || path == "/v1/dev/collect" {
		maxResponse = MaxCollectResponse
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return err
	}
	if int64(len(payload)) > maxResponse {
		return errors.New("node response exceeds limit")
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("invalid node response: %w", err)
	}
	return nil
}
func (c *Client) Prepare(ctx context.Context, request StageRequest) (StageReceipt, error) {
	var receipt StageReceipt
	err := c.send(ctx, "/v1/tasks/prepare", request, &receipt)
	if err != nil {
		return StageReceipt{}, err
	}
	if receipt.TaskID != request.TaskID || receipt.SkillRef != request.SkillRef || receipt.PolicyRevision != request.PolicyRevision || receipt.LeaseID == "" {
		return StageReceipt{}, errors.New("invalid node staging receipt")
	}
	return receipt, nil
}
func (c *Client) Dispose(ctx context.Context, leaseID, userID string, cancel bool) error {
	path := "/v1/tasks/dispose"
	if cancel {
		path = "/v1/tasks/cancel"
	}
	var reply struct{ Status string }
	if err := c.send(ctx, path, StageControl{LeaseID: leaseID, UserID: userID}, &reply); err != nil {
		return err
	}
	if reply.Status != "disposed" {
		return errors.New("invalid node stage disposal")
	}
	return nil
}

func (c *Client) Execute(ctx context.Context, req ExecuteRequest) (sandboxoci.Result, error) {
	var response ExecuteReply
	err := c.send(ctx, "/v1/dev/execute", req, &response)
	if err != nil {
		return sandboxoci.Result{}, err
	}
	return response.Result, nil
}
func (c *Client) Collect(ctx context.Context, control StageControl) ([]sandboxtransfer.OutputFile, error) {
	var response CollectReply
	if err := c.send(ctx, "/v1/dev/collect", control, &response); err != nil {
		return nil, err
	}
	total := 0
	for _, f := range response.Files {
		sum := sha256.Sum256(f.Content)
		total += len(f.Content)
		if total > 8<<20 || f.Path == "" || filepath.Base(f.Path) != f.Path || f.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, errors.New("invalid Node output digest or path")
		}
	}
	return response.Files, nil
}

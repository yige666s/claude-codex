package bridge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	toolkit "claude-codex/internal/harness/tools"
)

type runnerStub struct {
	mu       sync.Mutex
	sessions map[string]SessionInfo
}

func newRunnerStub() *runnerStub {
	return &runnerStub{sessions: make(map[string]SessionInfo)}
}

func (r *runnerStub) RunPrompt(_ context.Context, workingDir, prompt string) (string, error) {
	return workingDir + ":" + prompt, nil
}

func (r *runnerStub) ListTools(_ context.Context, workingDir string) ([]toolkit.Descriptor, error) {
	return []toolkit.Descriptor{{Name: "echo", Description: workingDir}}, nil
}

func (r *runnerStub) CreateSession(_ context.Context, workingDir string) (*SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := SessionInfo{
		ID:         "session-" + strings.ReplaceAll(workingDir, "/", "_"),
		WorkingDir: workingDir,
		StartedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	r.sessions[session.ID] = session
	return &session, nil
}

func (r *runnerStub) RunSessionPrompt(_ context.Context, sessionID, prompt string) (*SessionPromptResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	session.MessageCount++
	session.LastUserMessage = prompt
	session.UpdatedAt = time.Now().UTC()
	r.sessions[sessionID] = session
	return &SessionPromptResult{
		Output:  session.WorkingDir + ":" + prompt,
		Session: session,
	}, nil
}

func (r *runnerStub) GetSession(_ context.Context, sessionID string) (*SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	return &session, nil
}

func (r *runnerStub) ListSessions(_ context.Context, workingDir string) ([]SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SessionInfo, 0, len(r.sessions))
	for _, session := range r.sessions {
		if workingDir == "" || session.WorkingDir == workingDir {
			out = append(out, session)
		}
	}
	return out, nil
}

func (r *runnerStub) DeleteSession(_ context.Context, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionID)
	return nil
}

func TestServerServe(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := bytes.NewBuffer(nil)
	request := Request{ID: 1, Method: MethodRunPrompt, WorkingDir: "/tmp/project", Prompt: "hello"}
	if err := json.NewEncoder(in).Encode(request); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, newRunnerStub())
	if err := server.Serve(context.Background(), in, out); err != nil {
		t.Fatalf("serve bridge: %v", err)
	}

	var response Response
	if err := json.NewDecoder(out).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), "hello") {
		t.Fatalf("unexpected response: %s", string(response.Result))
	}
}

func TestServerServeUsesParamsPayload(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := bytes.NewBuffer(nil)
	request := Request{
		ID:     2,
		Method: MethodRunPrompt,
		Params: json.RawMessage(`{"working_dir":"/tmp/params","prompt":"from-params"}`),
	}
	if err := json.NewEncoder(in).Encode(request); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, newRunnerStub())
	if err := server.Serve(context.Background(), in, out); err != nil {
		t.Fatalf("serve bridge: %v", err)
	}

	var response Response
	if err := json.NewDecoder(out).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), "/tmp/params:from-params") {
		t.Fatalf("unexpected response: %s", string(response.Result))
	}
}

func TestServerListToolsUsesParamsPayload(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := bytes.NewBuffer(nil)
	request := Request{
		ID:     3,
		Method: MethodListTools,
		Params: json.RawMessage(`{"working_dir":"/tmp/tools"}`),
	}
	if err := json.NewEncoder(in).Encode(request); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, newRunnerStub())
	if err := server.Serve(context.Background(), in, out); err != nil {
		t.Fatalf("serve bridge: %v", err)
	}

	var response Response
	if err := json.NewDecoder(out).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response.Result), `"echo"`) {
		t.Fatalf("unexpected response: %s", string(response.Result))
	}
}

func TestServerSessionLifecycle(t *testing.T) {
	runner := newRunnerStub()
	server := NewServer("secret", runner)
	ctx := context.Background()

	createResp := server.handle(ctx, Request{
		ID:     4,
		Method: MethodCreateSession,
		Secret: "secret",
		Params: json.RawMessage(`{"working_dir":"/tmp/session"}`),
	})
	if createResp.Error != "" {
		t.Fatalf("create_session error: %s", createResp.Error)
	}
	var created CreateSessionResult
	if err := json.Unmarshal(createResp.Result, &created); err != nil {
		t.Fatal(err)
	}
	if created.Session.ID == "" {
		t.Fatal("expected session id")
	}

	promptResp := server.handle(ctx, Request{
		ID:        5,
		Method:    MethodSessionPrompt,
		Secret:    "secret",
		SessionID: created.Session.ID,
		Prompt:    "next prompt",
	})
	if promptResp.Error != "" {
		t.Fatalf("session_prompt error: %s", promptResp.Error)
	}
	if !strings.Contains(string(promptResp.Result), "next prompt") {
		t.Fatalf("unexpected session prompt result: %s", string(promptResp.Result))
	}

	listResp := server.handle(ctx, Request{
		ID:         6,
		Method:     MethodListSessions,
		Secret:     "secret",
		WorkingDir: "/tmp/session",
	})
	if listResp.Error != "" {
		t.Fatalf("list_sessions error: %s", listResp.Error)
	}
	if !strings.Contains(string(listResp.Result), created.Session.ID) {
		t.Fatalf("unexpected list_sessions result: %s", string(listResp.Result))
	}

	deleteResp := server.handle(ctx, Request{
		ID:        7,
		Method:    MethodDeleteSession,
		Secret:    "secret",
		SessionID: created.Session.ID,
	})
	if deleteResp.Error != "" {
		t.Fatalf("delete_session error: %s", deleteResp.Error)
	}
	if _, ok := runner.sessions[created.Session.ID]; ok {
		t.Fatal("expected session to be deleted")
	}
}

func TestBridgeClientRoundTrip(t *testing.T) {
	reqReader, reqWriter := io.Pipe()
	respReader, respWriter := io.Pipe()
	server := NewServer("bridge-secret", newRunnerStub())

	done := make(chan error, 1)
	go func() {
		done <- server.Serve(context.Background(), reqReader, respWriter)
	}()

	client := NewClient("bridge-secret", respReader, reqWriter)
	result, err := client.RunPrompt(context.Background(), "/tmp/client", "hi")
	if err != nil {
		t.Fatalf("client run prompt: %v", err)
	}
	if result.Output != "/tmp/client:hi" {
		t.Fatalf("unexpected output: %+v", result)
	}
	_ = reqWriter.Close()
	_ = respReader.Close()
	if err := <-done; err != nil {
		t.Fatalf("server serve: %v", err)
	}
}

func TestDecodeWorkSecretAndURLs(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"session_ingress_token":"tok","api_base_url":"https://api.example.com"}`))
	secret, err := DecodeWorkSecret(encoded)
	if err != nil {
		t.Fatalf("DecodeWorkSecret: %v", err)
	}
	if secret.SessionIngressToken != "tok" {
		t.Fatalf("unexpected token: %+v", secret)
	}
	if got := BuildSDKURL("https://api.example.com", "session_123"); got != "wss://api.example.com/v1/session_ingress/ws/session_123" {
		t.Fatalf("unexpected sdk url: %s", got)
	}
	if got := BuildCCRV2SDKURL("https://api.example.com/", "session_123"); got != "https://api.example.com/v1/code/sessions/session_123" {
		t.Fatalf("unexpected ccr url: %s", got)
	}
	if !SameSessionID("session_1234abcd", "cse_1234abcd") {
		t.Fatal("expected session ids with same suffix to match")
	}
}

func TestRegisterWorkerAndJWTUtils(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/session/worker/register" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			body, _ := json.Marshal(map[string]string{"worker_epoch": "42"})
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       ioutil.NopCloser(bytes.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	epoch, err := RegisterWorker(context.Background(), client, "https://bridge.test/session", "access-token")
	if err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}
	if epoch != 42 {
		t.Fatalf("unexpected epoch: %d", epoch)
	}

	payload, ok := DecodeJWTPayload("sk-ant-si-header.eyJleHAiOjQyfQ.sig")
	if !ok {
		t.Fatal("expected payload to decode")
	}
	if payload["exp"].(float64) != 42 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	exp, ok := DecodeJWTExpiry("header.eyJleHAiOjEyM30.sig")
	if !ok || exp != 123 {
		t.Fatalf("unexpected expiry: %d %v", exp, ok)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func TestTokenRefreshSchedulerAndInboundMessages(t *testing.T) {
	refreshed := make(chan string, 2)
	scheduler := NewTokenRefreshScheduler(
		func() (string, error) { return "oauth-token", nil },
		func(sessionID, accessToken string) {
			refreshed <- sessionID + ":" + accessToken
		},
	)
	scheduler.refreshBuffer = 10 * time.Millisecond
	scheduler.fallbackRefreshDelay = 20 * time.Millisecond
	scheduler.retryDelay = 10 * time.Millisecond

	scheduler.ScheduleFromExpiresIn("session-1", 30*time.Millisecond)

	select {
	case got := <-refreshed:
		if got != "session-1:oauth-token" {
			t.Fatalf("unexpected refresh callback: %s", got)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected refresh callback")
	}
	scheduler.CancelAll()

	msg := InboundMessage{
		Type: "user",
		UUID: "uuid-1",
		Message: &InboundPayload{
			Content: []map[string]any{
				{
					"type": "image",
					"source": map[string]any{
						"type":      "base64",
						"mediaType": "image/jpeg",
						"data":      base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8, 0xFF}),
					},
				},
			},
		},
	}
	fields, ok := ExtractInboundMessageFields(msg)
	if !ok {
		t.Fatal("expected inbound fields")
	}
	blocks := fields.Content.([]map[string]any)
	source := blocks[0]["source"].(map[string]any)
	if source["media_type"] != "image/jpeg" {
		t.Fatalf("expected normalized media_type, got %#v", source)
	}
}

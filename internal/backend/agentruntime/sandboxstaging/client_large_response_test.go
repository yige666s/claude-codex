package sandboxstaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxoci"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

type deadlineTransport func(*http.Request) (*http.Response, error)

func (f deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientExecutionDeadlineAllowsLongRunningCommand(t *testing.T) {
	transport := &http.Client{Transport: deadlineTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("node request has no deadline")
		} else {
			remaining := time.Until(deadline)
			if r.URL.Path == "/v1/dev/execute" && remaining < 60*time.Minute {
				t.Errorf("execution deadline too short: %s", remaining)
			}
			if r.URL.Path == "/v1/dev/collect" && remaining > nodeRequestTimeout {
				t.Errorf("collection deadline too long: %s", remaining)
			}
		}
		body := `{"result":{}}`
		if r.URL.Path == "/v1/dev/collect" {
			body = `{"files":[]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	client, err := NewClient("http://127.0.0.1:8080", []byte(strings.Repeat("k", 32)), transport)
	if err != nil {
		t.Fatal(err)
	}
	defaultClient, err := NewClient("http://127.0.0.1:8080", []byte(strings.Repeat("k", 32)), nil)
	if err != nil || defaultClient.http.Timeout != 0 {
		t.Fatalf("default HTTP timeout overrides request deadline: %v, %v", defaultClient, err)
	}
	control := StageControl{LeaseID: "test", UserID: "alice"}
	if _, err := client.Execute(context.Background(), ExecuteRequest{StageControl: control, ExecutionID: "test", Script: []byte("echo test")}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Collect(context.Background(), control); err != nil {
		t.Fatal(err)
	}
}

func TestClientCollectAcceptsBoundedOfficeArtifactAboveLegacyLimit(t *testing.T) {
	content := bytes.Repeat([]byte("fake-docx-data"), 2048)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/dev/collect" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(CollectReply{Files: []sandboxtransfer.OutputFile{{Path: "report.docx", Content: content, SHA256: sandboxArtifactDigest(content)}}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := client.Collect(context.Background(), StageControl{LeaseID: "test", UserID: "alice"})
	if err != nil || len(files) != 1 || !bytes.Equal(files[0].Content, content) {
		t.Fatalf("bounded large collect failed: size=%d err=%v", len(files), err)
	}
}
func TestClientRejectsOversizedAndMalformedNodeResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/dev/collect" {
			_, _ = w.Write(bytes.Repeat([]byte("x"), MaxCollectResponse+1))
			return
		}
		_, _ = w.Write([]byte("{}{}"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Collect(context.Background(), StageControl{LeaseID: "test", UserID: "alice"}); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("oversize accepted: %v", err)
	}
	_, err = client.Execute(context.Background(), ExecuteRequest{StageControl: StageControl{LeaseID: "test", UserID: "alice"}, ExecutionID: "test", Script: []byte("echo fake")})
	if err == nil {
		t.Fatal("extra JSON accepted")
	}
}
func TestClientExecOutputAboveLegacyLimit(t *testing.T) {
	expected := strings.Repeat("fake output", 3000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ExecuteReply{Result: sandboxoci.Result{Stdout: expected}})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, []byte(strings.Repeat("k", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Execute(context.Background(), ExecuteRequest{StageControl: StageControl{LeaseID: "test", UserID: "alice"}, ExecutionID: "test", Script: []byte("echo fake")})
	if err != nil || result.Stdout != expected {
		t.Fatalf("large exec response: %d %v", len(result.Stdout), err)
	}
}

func sandboxArtifactDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type runnerStub struct{}

func (runnerStub) RunPrompt(_ context.Context, workingDir, prompt string) (string, error) {
	return workingDir + ":" + prompt, nil
}

func (runnerStub) ListTools(_ context.Context, _ string) ([]toolkit.Descriptor, error) {
	return []toolkit.Descriptor{{Name: "echo"}}, nil
}

func TestServerServe(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := bytes.NewBuffer(nil)
	request := Request{ID: 1, Method: "run_prompt", WorkingDir: "/tmp/project", Prompt: "hello"}
	if err := json.NewEncoder(in).Encode(request); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, runnerStub{})
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
		Method: "run_prompt",
		Params: json.RawMessage(`{"working_dir":"/tmp/params","prompt":"from-params"}`),
	}
	if err := json.NewEncoder(in).Encode(request); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, runnerStub{})
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
		Method: "list_tools",
		Params: json.RawMessage(`{"working_dir":"/tmp/tools"}`),
	}
	if err := json.NewEncoder(in).Encode(request); err != nil {
		t.Fatal(err)
	}

	server := NewServer(nil, runnerStub{})
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

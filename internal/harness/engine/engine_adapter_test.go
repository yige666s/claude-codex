package engine

import (
	"context"
	"encoding/json"
	"testing"

	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type runtimeSpy struct {
	descriptorsCalled bool
	executeCalled     bool
	runCalls          []bool
	streamCalls       []bool
	result            Result
}

func (s *runtimeSpy) Descriptors() []toolkit.Descriptor {
	s.descriptorsCalled = true
	return []toolkit.Descriptor{{Name: "spy_tool"}}
}

func (s *runtimeSpy) ExecuteTool(context.Context, string, json.RawMessage) (toolkit.Result, error) {
	s.executeCalled = true
	return toolkit.Result{Output: "ok"}, nil
}

func (s *runtimeSpy) Run(_ context.Context, _ *state.Session, _ interface{}, recordUserMessage bool) (Result, error) {
	s.runCalls = append(s.runCalls, recordUserMessage)
	return s.result, nil
}

func (s *runtimeSpy) RunStream(_ context.Context, _ *state.Session, _ interface{}, recordUserMessage bool, _ func(string)) (Result, error) {
	s.streamCalls = append(s.streamCalls, recordUserMessage)
	return s.result, nil
}

func TestEnginePublicMethodsDelegateToRuntime(t *testing.T) {
	engine := &Engine{}
	spy := &runtimeSpy{
		result: Result{Output: "delegated"},
	}
	engine.runner = spy

	descriptors := engine.Descriptors()
	if !spy.descriptorsCalled {
		t.Fatal("expected descriptors to delegate to runtime")
	}
	if len(descriptors) != 1 || descriptors[0].Name != "spy_tool" {
		t.Fatalf("unexpected descriptors: %#v", descriptors)
	}

	if _, err := engine.ExecuteTool(context.Background(), "spy_tool", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if !spy.executeCalled {
		t.Fatal("expected ExecuteTool to delegate to runtime")
	}

	session := state.NewSession(t.TempDir())
	result, err := engine.Run(context.Background(), session, "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "delegated" {
		t.Fatalf("unexpected run result: %#v", result)
	}

	_, err = engine.RunGeneratedPrompt(context.Background(), session, "generated")
	if err != nil {
		t.Fatalf("RunGeneratedPrompt() error = %v", err)
	}

	if len(spy.runCalls) != 2 {
		t.Fatalf("expected two delegated run calls, got %d", len(spy.runCalls))
	}
	if !spy.runCalls[0] {
		t.Fatal("expected Run() to delegate with recordUserMessage=true")
	}
	if spy.runCalls[1] {
		t.Fatal("expected RunGeneratedPrompt() to delegate with recordUserMessage=false")
	}

	if _, err := engine.RunStream(context.Background(), session, "stream", nil); err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if _, err := engine.RunGeneratedPromptStream(context.Background(), session, "generated stream", nil); err != nil {
		t.Fatalf("RunGeneratedPromptStream() error = %v", err)
	}
	if len(spy.streamCalls) != 2 {
		t.Fatalf("expected two delegated stream calls, got %d", len(spy.streamCalls))
	}
	if !spy.streamCalls[0] {
		t.Fatal("expected RunStream() to delegate with recordUserMessage=true")
	}
	if spy.streamCalls[1] {
		t.Fatal("expected RunGeneratedPromptStream() to delegate with recordUserMessage=false")
	}
}

func TestBuildPermissionRequestUsesReusableBashPrefix(t *testing.T) {
	input := json.RawMessage(`{"command":"NODE_ENV=test timeout 10 npm run build > out.log"}`)
	request := buildPermissionRequest("bash", "execute", input)

	if request.Metadata["command_prefix"] != "npm run" {
		t.Fatalf("command_prefix = %q, want npm run", request.Metadata["command_prefix"])
	}
	if request.Metadata["access"] != "write-or-exec" {
		t.Fatalf("access = %q, want write-or-exec", request.Metadata["access"])
	}
}

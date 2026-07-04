package agentruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"

	"github.com/gorilla/websocket"
)

func TestLiveBackendE2EAudioTranscriptionAndPersistence(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "test-token")
	upstream := newFakeGeminiLiveServer(t,
		fakeGeminiScenario{
			onAudioEnd: []map[string]any{
				fakeGeminiServerContent(map[string]any{
					"inputTranscription": map[string]any{"text": "你好"},
				}),
				fakeGeminiServerContent(map[string]any{
					"outputTranscription": map[string]any{"text": "你好，我在。"},
					"modelTurn": map[string]any{"parts": []any{
						map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": "AQID"}},
					}},
					"turnComplete": true,
				}),
			},
		},
	)
	defer upstream.Close()

	runtime, store, sessionID := newLiveE2ERuntime(t, upstream.URL(), nil)
	server := httptest.NewServer(NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil))
	defer server.Close()
	conn := dialLiveBackend(t, server.URL, sessionID)
	defer conn.Close()

	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_ready" }, "live_ready")
	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_setup_complete" }, "live_setup_complete")
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "audio", MIMEType: "audio/pcm;rate=16000", Data: "AAEC"})
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "audio_end"})

	input := expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "live_transcript" && event.Role == state.MessageRoleUser
	}, "input transcript")
	if input.Content != "你好" {
		t.Fatalf("input transcript = %q, want 你好", input.Content)
	}
	output := expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "live_transcript" && event.Role == state.MessageRoleAssistant
	}, "output transcript")
	if output.Content != "你好，我在。" {
		t.Fatalf("output transcript = %q, want 你好，我在。", output.Content)
	}
	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_audio" }, "live audio")
	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "message" && event.Role == state.MessageRoleAssistant && event.Content == "你好，我在。"
	}, "persisted assistant message event")
	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_response_end" }, "live response end")

	sentAudio := upstream.ExpectClientRealtimeInput(t, func(input map[string]any) bool {
		audio, _ := input["audio"].(map[string]any)
		return audio["mimeType"] == "audio/pcm;rate=16000" && audio["data"] == "AAEC"
	})
	if len(sentAudio) == 0 {
		t.Fatal("expected backend to send client audio to Gemini Live")
	}

	saved, err := store.Get(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	assertLiveMessagesPersisted(t, saved.Messages, "你好", "你好，我在。")
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "close"})
}

func TestLiveBackendE2ESendsInitialHistoryAfterSetupComplete(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "test-token")
	upstream := newFakeGeminiLiveServer(t, fakeGeminiScenario{})
	defer upstream.Close()

	runtime, store, sessionID := newLiveE2ERuntime(t, upstream.URL(), nil)
	session, err := store.Get(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	session.AddUserMessage("之前的问题")
	session.AddAssistantMessage("之前的回答")
	if err := store.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	server := httptest.NewServer(NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil))
	defer server.Close()
	conn := dialLiveBackend(t, server.URL, sessionID)
	defer conn.Close()

	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_ready" }, "live_ready")
	initialHistory := upstream.ExpectClientContent(t, func(clientContent map[string]any) bool {
		turns, _ := clientContent["turns"].([]any)
		if len(turns) == 0 {
			return false
		}
		first, _ := turns[0].(map[string]any)
		parts, _ := first["parts"].([]any)
		if len(parts) == 0 {
			return false
		}
		part, _ := parts[0].(map[string]any)
		return first["role"] == "user" && part["text"] == "之前的问题" && clientContent["turnComplete"] == true
	})
	if len(initialHistory) == 0 {
		t.Fatal("expected initial history clientContent")
	}
	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_setup_complete" }, "live_setup_complete")
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "close"})
}

func TestLiveBackendE2EExplicitSlashSkillRouting(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "test-token")
	upstream := newFakeGeminiLiveServer(t,
		fakeGeminiScenario{
			onAudioEnd: []map[string]any{
				fakeGeminiServerContent(map[string]any{
					"inputTranscription":  map[string]any{"text": "/diagram 画一个登录流程"},
					"outputTranscription": map[string]any{"text": "我来生成。"},
					"modelTurn": map[string]any{"parts": []any{
						map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": "stale-audio"}},
					}},
					"turnComplete": true,
				}),
			},
		},
	)
	defer upstream.Close()
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "diagram",
		DisplayName:   "架构图",
		UserInvocable: true,
		GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
			return []skills.ContentBlock{{Type: "text", Text: "diagram prompt: " + args}}, nil
		},
	}}}
	runtime, store, sessionID := newLiveE2ERuntime(t, upstream.URL(), catalog)
	server := httptest.NewServer(NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil))
	defer server.Close()
	conn := dialLiveBackend(t, server.URL, sessionID)
	defer conn.Close()

	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_setup_complete" }, "live_setup_complete")
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "audio", Data: "AAEC"})
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "audio_end"})

	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "live_transcript" && event.Role == state.MessageRoleUser && strings.Contains(event.Content, "/diagram")
	}, "skill input transcript")
	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "message" && event.Role == state.MessageRoleUser && strings.Contains(event.Content, "/diagram")
	}, "skill user message")
	start := expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "tool_call_start" && strings.HasPrefix(event.Tool, "/diagram ")
	}, "live skill start")
	if !strings.HasPrefix(start.Tool, "/diagram ") {
		t.Fatalf("skill command = %q, want /diagram", start.Tool)
	}
	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "tool_call_result" && strings.Contains(event.Summary, "diagram prompt: 画一个登录流程")
	}, "live skill result")
	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "message" && event.Role == state.MessageRoleAssistant && strings.Contains(event.Content, "diagram prompt: 画一个登录流程")
	}, "skill assistant message")

	saved, err := store.Get(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	assertLiveMessagesPersisted(t, saved.Messages, "/diagram 画一个登录流程", "diagram prompt: 画一个登录流程")
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "close"})
}

func TestLiveBackendE2ENativeFunctionCallingRoutesSkill(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "test-token")
	upstream := newFakeGeminiLiveServer(t,
		fakeGeminiScenario{
			onAudioEnd: []map[string]any{
				fakeGeminiServerContent(map[string]any{
					"inputTranscription": map[string]any{"text": "帮我画一只橙色的中华田园猫"},
				}),
				{
					"toolCall": map[string]any{
						"functionCalls": []any{
							map[string]any{
								"id":   "call-1",
								"name": "run_skill",
								"args": map[string]any{
									"skill": "vertex-image-artifact",
									"args":  "一只橙色的中华田园猫",
								},
							},
						},
					},
				},
			},
			onToolResponse: []map[string]any{
				fakeGeminiServerContent(map[string]any{
					"outputTranscription": map[string]any{"text": "已开始生成图片。"},
					"turnComplete":        true,
				}),
			},
		},
	)
	defer upstream.Close()
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{{
		Name:          "vertex-image-artifact",
		DisplayName:   "图片生成",
		Description:   "Generate images from natural language prompts.",
		UserInvocable: true,
		RunAsJob:      true,
		Metadata: map[string]any{
			"agentapi": map[string]any{"produces_artifacts": true},
		},
		GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
			return []skills.ContentBlock{{Type: "text", Text: "image prompt: " + args}}, nil
		},
	}}}
	runtime, store, sessionID := newLiveE2ERuntime(t, upstream.URL(), catalog)
	jobs := NewMemoryJobStore()
	runtime.SetJobStore(jobs)
	server := httptest.NewServer(NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil))
	defer server.Close()
	conn := dialLiveBackend(t, server.URL, sessionID)
	defer conn.Close()

	expectLiveEvent(t, conn, func(event Event) bool { return event.Type == "live_setup_complete" }, "live_setup_complete")
	upstream.ExpectClientSetup(t, func(setup map[string]any) bool {
		return liveSetupHasRunSkillTool(setup)
	})
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "audio", Data: "AAEC"})
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "audio_end"})

	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "live_transcript" && event.Role == state.MessageRoleUser && strings.Contains(event.Content, "中华田园猫")
	}, "function-call input transcript")
	start := expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "tool_call_start" && strings.HasPrefix(event.Tool, "/vertex-image-artifact ")
	}, "native function live skill start")
	if !strings.Contains(start.Tool, "中华田园猫") {
		t.Fatalf("skill command lost args: %q", start.Tool)
	}
	jobEvent := expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "job" && event.Job != nil && event.Job.Type == "skill"
	}, "native function skill job")
	waitForLiveTestJob(t, jobs, "alice", jobEvent.Job.ID)
	upstream.ExpectClientToolResponse(t, func(toolResponse map[string]any) bool {
		responses, _ := toolResponse["functionResponses"].([]any)
		if len(responses) != 1 {
			return false
		}
		response, _ := responses[0].(map[string]any)
		body, _ := response["response"].(map[string]any)
		result, _ := body["result"].(string)
		return response["id"] == "call-1" && response["name"] == "run_skill" && strings.Contains(result, "created docx file")
	})
	expectLiveEvent(t, conn, func(event Event) bool {
		return event.Type == "message" && event.Role == state.MessageRoleAssistant && event.Content == "已开始生成图片。"
	}, "native function assistant acknowledgement")

	saved, err := store.Get(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	var sawOriginalUser bool
	for _, message := range saved.Messages {
		if !message.Hidden && message.Role == state.MessageRoleUser && strings.Contains(message.Content, "中华田园猫") {
			sawOriginalUser = true
			break
		}
	}
	if !sawOriginalUser {
		t.Fatalf("expected original live utterance to be persisted, messages=%#v", saved.Messages)
	}
	writeLiveClientEvent(t, conn, LiveClientEvent{Type: "close"})
}

func TestLiveBackendE2EDisconnectReconnectsSameSession(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "test-token")
	upstream := newFakeGeminiLiveServer(t,
		fakeGeminiScenario{closeAfterSetup: true},
		fakeGeminiScenario{
			onAudioEnd: []map[string]any{
				fakeGeminiServerContent(map[string]any{
					"inputTranscription":  map[string]any{"text": "重连后继续"},
					"outputTranscription": map[string]any{"text": "已恢复连接。"},
					"turnComplete":        true,
				}),
			},
		},
	)
	defer upstream.Close()
	runtime, store, sessionID := newLiveE2ERuntime(t, upstream.URL(), nil)
	server := httptest.NewServer(NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil))
	defer server.Close()

	first := dialLiveBackend(t, server.URL, sessionID)
	expectLiveEvent(t, first, func(event Event) bool { return event.Type == "live_setup_complete" }, "first setup")
	expectLiveEvent(t, first, func(event Event) bool { return event.Type == "done" }, "first disconnect done")
	first.Close()

	second := dialLiveBackend(t, server.URL, sessionID)
	defer second.Close()
	expectLiveEvent(t, second, func(event Event) bool { return event.Type == "live_setup_complete" }, "second setup")
	writeLiveClientEvent(t, second, LiveClientEvent{Type: "audio", Data: "AQID"})
	writeLiveClientEvent(t, second, LiveClientEvent{Type: "audio_end"})
	expectLiveEvent(t, second, func(event Event) bool {
		return event.Type == "message" && event.Role == state.MessageRoleAssistant && event.Content == "已恢复连接。"
	}, "reconnected assistant message")

	saved, err := store.Get(context.Background(), "alice", sessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	assertLiveMessagesPersisted(t, saved.Messages, "重连后继续", "已恢复连接。")
	writeLiveClientEvent(t, second, LiveClientEvent{Type: "close"})
}

type fakeGeminiScenario struct {
	onAudioEnd      []map[string]any
	onToolResponse  []map[string]any
	closeAfterSetup bool
}

type fakeGeminiLiveServer struct {
	t         *testing.T
	server    *httptest.Server
	upgrader  websocket.Upgrader
	scenarios chan fakeGeminiScenario
	received  chan map[string]any
}

func newFakeGeminiLiveServer(t *testing.T, scenarios ...fakeGeminiScenario) *fakeGeminiLiveServer {
	t.Helper()
	fake := &fakeGeminiLiveServer{
		t:         t,
		upgrader:  websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		scenarios: make(chan fakeGeminiScenario, len(scenarios)),
		received:  make(chan map[string]any, 64),
	}
	for _, scenario := range scenarios {
		fake.scenarios <- scenario
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	return fake
}

func (s *fakeGeminiLiveServer) URL() string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http")
}

func (s *fakeGeminiLiveServer) Close() {
	s.server.Close()
}

func (s *fakeGeminiLiveServer) ExpectClientRealtimeInput(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case message := <-s.received:
			realtime, _ := message["realtimeInput"].(map[string]any)
			if realtime != nil && match(realtime) {
				return realtime
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching Gemini realtimeInput")
		}
	}
}

func (s *fakeGeminiLiveServer) ExpectClientContent(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case message := <-s.received:
			clientContent, _ := message["clientContent"].(map[string]any)
			if clientContent != nil && match(clientContent) {
				return clientContent
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching Gemini clientContent")
		}
	}
}

func (s *fakeGeminiLiveServer) ExpectClientSetup(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case message := <-s.received:
			setup, _ := message["setup"].(map[string]any)
			if setup != nil && match(setup) {
				return setup
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching Gemini setup")
		}
	}
}

func (s *fakeGeminiLiveServer) ExpectClientToolResponse(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case message := <-s.received:
			toolResponse, _ := message["toolResponse"].(map[string]any)
			if toolResponse != nil && match(toolResponse) {
				return toolResponse
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching Gemini toolResponse")
		}
	}
}

func (s *fakeGeminiLiveServer) handle(w http.ResponseWriter, r *http.Request) {
	scenario, ok := <-s.scenarios
	if !ok {
		http.Error(w, "no fake Gemini scenario available", http.StatusInternalServerError)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.t.Errorf("upgrade fake Gemini Live websocket: %v", err)
		return
	}
	defer conn.Close()

	var setup map[string]any
	if err := conn.ReadJSON(&setup); err != nil {
		s.t.Errorf("read Gemini setup: %v", err)
		return
	}
	s.received <- setup
	if err := conn.WriteJSON(map[string]any{"setupComplete": map[string]any{}}); err != nil {
		s.t.Errorf("write Gemini setup complete: %v", err)
		return
	}
	if scenario.closeAfterSetup {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "test disconnect"), time.Now().Add(time.Second))
		return
	}
	for {
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				s.t.Logf("fake Gemini Live read ended: %v", err)
			}
			return
		}
		s.received <- message
		realtime, _ := message["realtimeInput"].(map[string]any)
		if ended, _ := realtime["audioStreamEnd"].(bool); ended {
			for _, response := range scenario.onAudioEnd {
				if err := conn.WriteJSON(response); err != nil {
					s.t.Errorf("write Gemini response: %v", err)
					return
				}
			}
		}
		if _, ok := message["toolResponse"].(map[string]any); ok {
			for _, response := range scenario.onToolResponse {
				if err := conn.WriteJSON(response); err != nil {
					s.t.Errorf("write Gemini tool response follow-up: %v", err)
					return
				}
			}
		}
	}
}

func fakeGeminiServerContent(content map[string]any) map[string]any {
	return map[string]any{"serverContent": content}
}

func liveSetupHasRunSkillTool(setup map[string]any) bool {
	tools, _ := setup["tools"].([]any)
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		declarations, _ := tool["functionDeclarations"].([]any)
		for _, raw := range declarations {
			declaration, _ := raw.(map[string]any)
			if declaration["name"] == "run_skill" {
				return true
			}
		}
	}
	return false
}

func newLiveE2ERuntime(t *testing.T, upstreamURL string, catalog SkillCatalog) (*Runtime, *FileSessionStore, string) {
	t.Helper()
	root := t.TempDir()
	store := NewFileSessionStore(root)
	runtime := NewRuntime(RuntimeConfig{
		DefaultWorkingDir: root,
		TurnTimeout:       time.Minute,
		Live: LiveConfig{
			Enabled:                    true,
			Provider:                   "vertex",
			VertexProjectID:            "test-project",
			VertexBaseURL:              upstreamURL,
			InputTranscriptionEnabled:  true,
			OutputTranscriptionEnabled: true,
			OutputAudioMIMEType:        "audio/pcm;rate=24000",
			SessionTimeout:             5 * time.Second,
		},
	}, store, nil, catalog, func(scope Scope) Runner {
		if scope.SkillName == "vertex-image-artifact" {
			return generatedArtifactFileRunner{workspace: scope.WorkingDir}
		}
		return echoRunner{}
	})
	runtime.SetArtifactService(NewArtifactService(newMemoryArtifactStore(), NewFileObjectStore(t.TempDir()), "artifacts"))
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return runtime, store, session.ID
}

func dialLiveBackend(t *testing.T, serverURL, sessionID string) *websocket.Conn {
	t.Helper()
	target := "ws" + strings.TrimPrefix(serverURL, "http") + "/v1/sessions/" + sessionID + "/live/ws"
	headers := http.Header{"X-User-ID": []string{"alice"}}
	conn, _, err := websocket.DefaultDialer.Dial(target, headers)
	if err != nil {
		t.Fatalf("dial live backend: %v", err)
	}
	return conn
}

func writeLiveClientEvent(t *testing.T, conn *websocket.Conn, event LiveClientEvent) {
	t.Helper()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteJSON(event); err != nil {
		t.Fatalf("write live client event: %v", err)
	}
}

func expectLiveEvent(t *testing.T, conn *websocket.Conn, match func(Event) bool, label string) Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		var event Event
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read %s event: %v", label, err)
		}
		if match(event) {
			return event
		}
		if event.Type == "error" {
			t.Fatalf("received error while waiting for %s: %s", label, event.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
	}
}

func assertLiveMessagesPersisted(t *testing.T, messages []state.Message, userText, assistantText string) {
	t.Helper()
	var sawUser, sawAssistant bool
	for _, message := range messages {
		if message.Role == state.MessageRoleUser && strings.Contains(message.Content, userText) {
			sawUser = true
		}
		if message.Role == state.MessageRoleAssistant && strings.Contains(message.Content, assistantText) {
			sawAssistant = true
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("persisted live messages missing user=%t assistant=%t messages=%#v", sawUser, sawAssistant, messages)
	}
}

package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
)

func TestLiveVertexWebSocketURL(t *testing.T) {
	got, err := liveVertexWebSocketURL(LiveConfig{VertexLocation: "us-central1"})
	if err != nil {
		t.Fatalf("liveVertexWebSocketURL: %v", err)
	}
	want := "wss://us-central1-aiplatform.googleapis.com/ws/google.cloud.aiplatform.v1beta1.LlmBidiService/BidiGenerateContent"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestNormalizeLiveConfigUsesLowLatencyVADDefaults(t *testing.T) {
	config := normalizeLiveConfig(LiveConfig{})
	if config.LiveVADStartSensitivity != "START_SENSITIVITY_HIGH" {
		t.Fatalf("start sensitivity = %q", config.LiveVADStartSensitivity)
	}
	if config.LiveVADEndSensitivity != "END_SENSITIVITY_HIGH" {
		t.Fatalf("end sensitivity = %q", config.LiveVADEndSensitivity)
	}
	if config.LiveVADPrefixPadding != 150*time.Millisecond {
		t.Fatalf("prefix padding = %s", config.LiveVADPrefixPadding)
	}
	if config.LiveVADSilenceDuration != 350*time.Millisecond {
		t.Fatalf("silence duration = %s", config.LiveVADSilenceDuration)
	}
}

func TestLiveSetupMessageDisablesProviderVADAndEnablesResumption(t *testing.T) {
	service := NewVertexLiveService(LiveConfig{
		Enabled:                   true,
		VertexProjectID:           "project-1",
		LiveVADPrefixPadding:      650 * time.Millisecond,
		LiveVADSilenceDuration:    1200 * time.Millisecond,
		LiveVADStartSensitivity:   "start_sensitivity_low",
		LiveVADEndSensitivity:     "end_sensitivity_low",
		InputTranscriptionEnabled: true,
	}, nil, nil)
	message := service.setupMessage(context.Background(), LiveRequest{UserID: "alice", SessionID: "session-1", ResumeHandle: "resume-1"})
	setup := message["setup"].(map[string]any)
	realtime := setup["realtimeInputConfig"].(map[string]any)
	detection := realtime["automaticActivityDetection"].(map[string]any)
	if detection["disabled"] != true {
		t.Fatalf("provider VAD should be disabled when frontend sends activity events: %#v", detection)
	}
	if realtime["turnCoverage"] != "TURN_INCLUDES_ONLY_ACTIVITY" {
		t.Fatalf("unexpected realtime input config: %#v", realtime)
	}
	resumption := setup["sessionResumption"].(map[string]any)
	if resumption["handle"] != "resume-1" {
		t.Fatalf("unexpected session resumption config: %#v", resumption)
	}
}

func TestLiveSetupMessageDisablesThinkingForDefault25Model(t *testing.T) {
	service := NewVertexLiveService(LiveConfig{
		Enabled:         true,
		VertexProjectID: "project-1",
		Model:           defaultLiveModel,
	}, nil, nil)
	message := service.setupMessage(context.Background(), LiveRequest{UserID: "alice", SessionID: "session-1"})
	setup := message["setup"].(map[string]any)
	generation := setup["generationConfig"].(map[string]any)
	thinking := generation["thinkingConfig"].(map[string]any)
	if thinking["thinkingBudget"] != 0 {
		t.Fatalf("unexpected thinking config: %#v", thinking)
	}
}

func TestLiveSetupMessageUsesMinimalThinkingFor31Model(t *testing.T) {
	service := NewVertexLiveService(LiveConfig{
		Enabled:         true,
		VertexProjectID: "project-1",
		Model:           "gemini-3.1-flash-live-preview",
	}, nil, nil)
	message := service.setupMessage(context.Background(), LiveRequest{UserID: "alice", SessionID: "session-1"})
	setup := message["setup"].(map[string]any)
	generation := setup["generationConfig"].(map[string]any)
	thinking := generation["thinkingConfig"].(map[string]any)
	if thinking["thinkingLevel"] != "MINIMAL" {
		t.Fatalf("unexpected thinking config: %#v", thinking)
	}
}

func TestLiveSetupMessageUsesPromptCache(t *testing.T) {
	recorder := &countingLiveRecorder{instruction: "cached instruction"}
	cache := NewMemoryLiveSetupPromptCache(time.Minute)
	service := NewVertexLiveService(LiveConfig{
		Enabled:         true,
		VertexProjectID: "project-1",
		Model:           defaultLiveModel,
	}, recorder, nil)
	service.SetSetupPromptCache(cache)
	req := LiveRequest{UserID: "alice", SessionID: "session-1"}
	first := service.setupMessage(context.Background(), req)
	second := service.setupMessage(context.Background(), req)
	if recorder.calls != 1 {
		t.Fatalf("LiveSystemInstruction calls = %d, want 1", recorder.calls)
	}
	for _, message := range []map[string]any{first, second} {
		setup := message["setup"].(map[string]any)
		systemInstruction := setup["systemInstruction"].(map[string]any)
		parts := systemInstruction["parts"].([]map[string]any)
		if parts[0]["text"] != "cached instruction" {
			t.Fatalf("unexpected system instruction: %#v", systemInstruction)
		}
	}
}

func TestLiveClientAudioEventToVertexPayload(t *testing.T) {
	payload, err := liveClientEventToVertexPayload(LiveClientEvent{Type: "audio", Data: "AAEC"}, "audio/pcm;rate=16000")
	if err != nil {
		t.Fatalf("liveClientEventToVertexPayload: %v", err)
	}
	realtime := payload["realtimeInput"].(map[string]any)
	audio := realtime["audio"].(map[string]any)
	if audio["mimeType"] != "audio/pcm;rate=16000" || audio["data"] != "AAEC" {
		t.Fatalf("unexpected audio payload: %#v", payload)
	}
}

type countingLiveRecorder struct {
	instruction string
	calls       int
}

func (r *countingLiveRecorder) LiveSystemInstruction(context.Context, string, string) string {
	r.calls++
	return r.instruction
}

func (r *countingLiveRecorder) RecordLiveTurn(context.Context, string, string, string, string, string) error {
	return nil
}

func TestLiveClientTraceEventIsIgnoredUpstream(t *testing.T) {
	payload, err := liveClientEventToVertexPayload(LiveClientEvent{Type: "client_trace", Content: `{"sample_rate":48000}`}, "audio/pcm;rate=16000")
	if err != nil {
		t.Fatalf("liveClientEventToVertexPayload: %v", err)
	}
	if payload != nil {
		t.Fatalf("client trace should not be forwarded to Vertex, got %#v", payload)
	}
}

func TestLiveClientDoneEventEndsAudioStream(t *testing.T) {
	payload, err := liveClientEventToVertexPayload(LiveClientEvent{Type: "audio_end_and_close"}, "audio/pcm;rate=16000")
	if err != nil {
		t.Fatalf("liveClientEventToVertexPayload: %v", err)
	}
	realtime := payload["realtimeInput"].(map[string]any)
	if realtime["audioStreamEnd"] != true {
		t.Fatalf("unexpected audio end payload: %#v", payload)
	}
}

func TestLiveInputNoiseFilterKeepsShortChinese(t *testing.T) {
	if liveIsNoisyInputTranscript("你好") {
		t.Fatal("short Chinese greeting should not be treated as noise")
	}
	for _, text := range []string{"喂", "hello", "hi"} {
		if liveIsNoisyInputTranscript(text) {
			t.Fatalf("%q should be kept as a meaningful greeting or wake word", text)
		}
	}
	for _, text := range []string{"嗯", "嗯嗯嗯", "呃", "那个", "ummm", "you know", "I mean", "调调调调"} {
		if !liveIsNoisyInputTranscript(text) {
			t.Fatalf("%q should be treated as noise", text)
		}
	}
	if !liveIsNoisyInputTranscript("调调调调") {
		t.Fatal("repeated ASR noise should be filtered")
	}
	for _, text := range []string{"帮我生成图片", "这个功能怎么用", "hello world"} {
		if liveIsNoisyInputTranscript(text) {
			t.Fatalf("%q should not be treated as noise", text)
		}
	}
}

func TestLiveErrorEventClassifiesCredentialFailures(t *testing.T) {
	event := liveErrorEvent("session-1", fmt.Errorf("live vertex access token is required: read GOOGLE_APPLICATION_CREDENTIALS: missing"))
	if event.Type != "error" || event.SessionID != "session-1" {
		t.Fatalf("unexpected event: %#v", event)
	}
	var data map[string]string
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatalf("decode error data: %v", err)
	}
	if data["code"] != "live_credentials_missing" {
		t.Fatalf("code = %q", data["code"])
	}
}

func TestLiveTurnAccumulatorEmitsAudioAndTranscripts(t *testing.T) {
	message := map[string]any{
		"serverContent": map[string]any{
			"inputTranscription":  map[string]any{"text": "你好"},
			"outputTranscription": map[string]any{"text": "你好，有什么可以帮你？"},
			"modelTurn": map[string]any{
				"parts": []any{
					map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": "AQID"}},
				},
			},
			"turnComplete": true,
		},
	}
	var turn liveTurnAccumulator
	events, complete, err := turn.consume(message, "")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !complete {
		t.Fatal("expected turn complete")
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4: %#v", len(events), events)
	}
	if events[0].Type != "live_transcript" || events[0].Role != state.MessageRoleUser || events[0].Content != "你好" {
		t.Fatalf("unexpected input transcript event: %#v", events[0])
	}
	if events[1].Type != "live_response_start" {
		t.Fatalf("unexpected response start event: %#v", events[1])
	}
	if events[3].Type != "live_audio" {
		t.Fatalf("unexpected audio event: %#v", events[3])
	}
	var audio map[string]string
	if err := json.Unmarshal(events[3].Data, &audio); err != nil {
		t.Fatalf("decode audio payload: %v", err)
	}
	if audio["mime_type"] != "audio/pcm;rate=24000" || audio["data"] != "AQID" {
		t.Fatalf("unexpected audio payload: %#v", audio)
	}
	userText, assistantText := turn.flush()
	if userText != "你好" || assistantText != "你好，有什么可以帮你？" {
		t.Fatalf("flush = %q/%q", userText, assistantText)
	}
}

func TestLiveTurnAccumulatorForwardsResumptionAndGoAway(t *testing.T) {
	var turn liveTurnAccumulator
	events, complete, err := turn.consume(map[string]any{
		"sessionResumptionUpdate": map[string]any{"newHandle": "handle-1"},
	}, "")
	if err != nil {
		t.Fatalf("consume resumption: %v", err)
	}
	if complete || len(events) != 1 || events[0].Type != "live_resumption_token" {
		t.Fatalf("unexpected resumption events complete=%t events=%#v", complete, events)
	}
	var resumption map[string]string
	if err := json.Unmarshal(events[0].Data, &resumption); err != nil {
		t.Fatalf("decode resumption payload: %v", err)
	}
	if resumption["handle"] != "handle-1" {
		t.Fatalf("unexpected resumption payload: %#v", resumption)
	}

	events, complete, err = turn.consume(map[string]any{
		"goAway": map[string]any{"timeLeft": "30s"},
	}, "")
	if err != nil {
		t.Fatalf("consume goAway: %v", err)
	}
	if complete || len(events) != 1 || events[0].Type != "live_go_away" {
		t.Fatalf("unexpected goAway events complete=%t events=%#v", complete, events)
	}
	var goAway map[string]string
	if err := json.Unmarshal(events[0].Data, &goAway); err != nil {
		t.Fatalf("decode goAway payload: %v", err)
	}
	if goAway["time_left"] != "30s" {
		t.Fatalf("unexpected goAway payload: %#v", goAway)
	}
}

func TestLiveTurnAccumulatorAcceptsSnakeCaseInlineAudio(t *testing.T) {
	message := map[string]any{
		"serverContent": map[string]any{
			"modelTurn": map[string]any{
				"parts": []any{
					map[string]any{"inline_data": map[string]any{"mime_type": "audio/L16;rate=24000", "data": "AQID"}},
				},
			},
		},
	}
	var turn liveTurnAccumulator
	events, _, err := turn.consume(message, "")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if len(events) != 2 || events[0].Type != "live_response_start" || events[1].Type != "live_audio" {
		t.Fatalf("unexpected events: %#v", events)
	}
	var audio map[string]string
	if err := json.Unmarshal(events[1].Data, &audio); err != nil {
		t.Fatalf("decode audio payload: %v", err)
	}
	if audio["mime_type"] != "audio/L16;rate=24000" || audio["data"] != "AQID" {
		t.Fatalf("unexpected audio payload: %#v", audio)
	}
}

func TestLiveTurnAccumulatorSuppressesInterruptedOutput(t *testing.T) {
	var turn liveTurnAccumulator
	events, complete, err := turn.consume(map[string]any{
		"serverContent": map[string]any{
			"outputTranscription": map[string]any{"text": "old answer"},
			"modelTurn": map[string]any{
				"parts": []any{
					map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": "AAAA"}},
				},
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("consume initial output: %v", err)
	}
	if complete {
		t.Fatal("initial output should not complete the turn")
	}
	if len(events) != 3 {
		t.Fatalf("initial events = %d, want response start, transcript and audio: %#v", len(events), events)
	}

	events, complete, err = turn.consume(map[string]any{
		"serverContent": map[string]any{
			"interrupted":         true,
			"outputTranscription": map[string]any{"text": "stale answer"},
			"modelTurn": map[string]any{
				"parts": []any{
					map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": "BBBB"}},
				},
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("consume interrupted output: %v", err)
	}
	if complete {
		t.Fatal("interrupted output should not complete the turn")
	}
	if len(events) != 1 || events[0].Type != "live_interrupted" {
		t.Fatalf("interrupted events should only notify interruption, got %#v", events)
	}

	events, complete, err = turn.consume(map[string]any{
		"serverContent": map[string]any{
			"outputTranscription": map[string]any{"text": "more stale answer"},
			"modelTurn": map[string]any{
				"parts": []any{
					map[string]any{"inlineData": map[string]any{"mimeType": "audio/pcm;rate=24000", "data": "CCCC"}},
				},
			},
			"turnComplete": true,
		},
	}, "")
	if err != nil {
		t.Fatalf("consume suppressed output: %v", err)
	}
	if !complete {
		t.Fatal("expected interrupted turn completion")
	}
	if len(events) != 0 {
		t.Fatalf("suppressed turn should not emit stale output, got %#v", events)
	}
	userText, assistantText := turn.flush()
	if userText != "" || assistantText != "" {
		t.Fatalf("interrupted turn should not record stale text, got %q/%q", userText, assistantText)
	}

	events, complete, err = turn.consume(map[string]any{
		"serverContent": map[string]any{
			"outputTranscription": map[string]any{"text": "new answer"},
			"turnComplete":        true,
		},
	}, "")
	if err != nil {
		t.Fatalf("consume new output: %v", err)
	}
	if !complete || len(events) != 2 || events[0].Type != "live_response_start" || events[1].Content != "new answer" {
		t.Fatalf("new turn should emit normally after interrupted turn completes, complete=%t events=%#v", complete, events)
	}
}

func TestRuntimeRecordLiveTurnPersistsMessagesAndMemory(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	memory := NewFileMemoryService(root)
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, memory, nil, func(Scope) Runner { return echoRunner{} })
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := runtime.RecordLiveTurn(context.Background(), "alice", session.ID, "I live in Shanghai", "Noted.", defaultLiveModel); err != nil {
		t.Fatalf("RecordLiveTurn: %v", err)
	}
	saved, err := store.Get(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(saved.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(saved.Messages))
	}
	if saved.Messages[0].Role != state.MessageRoleUser || saved.Messages[0].ModelID != defaultLiveModel {
		t.Fatalf("unexpected user message: %#v", saved.Messages[0])
	}
	items, err := memory.ListMemoryItems(context.Background(), "alice", MemoryItemFilter{})
	if err != nil {
		t.Fatalf("ListMemoryItems: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected live turn to trigger memory extraction")
	}
}

func TestRuntimeLiveSystemInstructionIncludesPublishedSkills(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{Name: "diagram", Description: "Create a diagram from a brief.", UserInvocable: true},
		{Name: "internal", Description: "Hidden operator workflow.", UserInvocable: false},
	}}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, nil, catalog, func(Scope) Runner { return echoRunner{} })
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	instruction := runtime.LiveSystemInstruction(context.Background(), "alice", session.ID)
	for _, want := range []string{
		"<skills>",
		"# Available Skills",
		"`/diagram`: Create a diagram from a brief.",
		"Live mode has access to a `run_skill` function",
		"call `run_skill` with the exact skill name",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("LiveSystemInstruction missing %q:\n%s", want, instruction)
		}
	}
	if strings.Contains(instruction, "`/internal`") || strings.Contains(instruction, "Hidden operator workflow") {
		t.Fatalf("LiveSystemInstruction should not include non-user-invocable skills:\n%s", instruction)
	}
}

func TestRuntimeLiveSkillCommandParsesExplicitSpeech(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{Name: "diagram", DisplayName: "架构图", UserInvocable: true},
	}}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, nil, catalog, func(Scope) Runner { return echoRunner{} })
	if _, err := runtime.CreateSession(context.Background(), "alice", root); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	command, ok := runtime.liveExplicitSkillCommand("请使用 架构图 技能帮我画一个服务拓扑")
	if !ok {
		t.Fatal("expected live speech to be recognized as skill command")
	}
	if command != "/diagram 画一个服务拓扑" {
		t.Fatalf("command = %q", command)
	}
}

func TestRuntimeLiveSkillCommandUsesModelSelection(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{
			Name:          "diagram",
			DisplayName:   "架构图",
			Description:   "Create diagrams from natural language process or architecture requests.",
			UserInvocable: true,
			GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
				return []skills.ContentBlock{{Type: "text", Text: "diagram prompt: " + args}}, nil
			},
		},
	}}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, nil, catalog, func(scope Scope) Runner {
		if scope.SkillScoped {
			return echoRunner{}
		}
		return liveSkillSelectorRunner{output: `{"action":"skill_call","skill":"diagram","args":"登录流程方案","confidence":0.91,"reason":"user asked for a process plan"}`}
	})
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sink := &collectSink{}

	handled, err := runtime.ExecuteLiveSkillCommand(context.Background(), "alice", session.ID, "我需要一个登录流程方案", sink)
	if err != nil {
		t.Fatalf("ExecuteLiveSkillCommand: %v", err)
	}
	if !handled {
		t.Fatal("expected model-selected live skill command to be handled")
	}
	var sawAssistant bool
	for _, event := range sink.events {
		if event.Type == "message" && event.Role == state.MessageRoleAssistant && strings.Contains(event.Content, "diagram prompt: 登录流程方案") {
			sawAssistant = true
			break
		}
	}
	if !sawAssistant {
		t.Fatalf("expected model-selected skill result message, events=%#v", sink.events)
	}
}

func TestRuntimeExecuteLiveSkillFunctionCallRunsArtifactJob(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	jobs := NewMemoryJobStore()
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{
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
		},
	}}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, nil, catalog, func(Scope) Runner { return echoRunner{} })
	runtime.SetJobStore(jobs)
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sink := &collectSink{}

	utterance := "帮我画一只橙色的中华田园猫，在院子里"
	handled, output, err := runtime.ExecuteLiveSkillFunctionCall(context.Background(), "alice", session.ID, "vertex-image-artifact", utterance, utterance, sink)
	if err != nil {
		t.Fatalf("ExecuteLiveSkillFunctionCall: %v", err)
	}
	if !handled {
		t.Fatal("expected live function artifact request to be handled")
	}
	if !strings.Contains(output, "Skill job started.") {
		t.Fatalf("output = %q", output)
	}
	var routedJob *Job
	for _, event := range sink.events {
		if event.Type == "job" {
			routedJob = event.Job
			break
		}
	}
	if routedJob == nil {
		t.Fatalf("expected routed skill job, events=%#v", sink.events)
	}
	if routedJob.Content != "/vertex-image-artifact "+utterance {
		t.Fatalf("job content = %q", routedJob.Content)
	}
	waitForLiveTestJob(t, jobs, "alice", routedJob.ID)
}

func TestRuntimeLiveSkillCommandUsesRecentContextForArtifactFollowup(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	jobs := NewMemoryJobStore()
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{
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
		},
	}}
	var selectorPrompt string
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, nil, catalog, func(scope Scope) Runner {
		if scope.SkillScoped {
			return echoRunner{}
		}
		return contextAwareLiveSkillSelectorRunner{prompt: &selectorPrompt}
	})
	runtime.SetJobStore(jobs)
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	session.AddUserMessage("帮我生成一张狗的图片")
	session.AddAssistantMessage("想画什么样的狗？")
	if err := store.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sink := &collectSink{}

	handled, err := runtime.ExecuteLiveSkillCommand(context.Background(), "alice", session.ID, "你自己决定", sink)
	if err != nil {
		t.Fatalf("ExecuteLiveSkillCommand: %v", err)
	}
	if !handled {
		t.Fatal("expected follow-up image request to be routed to a live skill")
	}
	if !strings.Contains(selectorPrompt, "Recent conversation:") || !strings.Contains(selectorPrompt, "帮我生成一张狗的图片") {
		t.Fatalf("selector prompt did not include recent context:\n%s", selectorPrompt)
	}
	var routedJob *Job
	var sawStart bool
	for _, event := range sink.events {
		if event.Type == "live_skill_start" {
			sawStart = true
		}
		if event.Type == "job" {
			routedJob = event.Job
		}
	}
	if !sawStart || routedJob == nil {
		t.Fatalf("expected live skill job events, events=%#v", sink.events)
	}
	if routedJob.Type != "skill" || routedJob.Content != "/vertex-image-artifact 一张狗的图片，风格由系统决定" {
		t.Fatalf("unexpected routed job: %#v", routedJob)
	}
	waitForLiveTestJob(t, jobs, "alice", routedJob.ID)
}

func waitForLiveTestJob(t *testing.T, jobs *MemoryJobStore, userID, jobID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		stored, err := jobs.GetJob(context.Background(), userID, jobID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if stored.Status == JobStatusSucceeded || stored.Status == JobStatusFailed || stored.Status == JobStatusCancelled {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not finish: %#v", stored)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type liveSkillSelectorRunner struct {
	output string
}

func (r liveSkillSelectorRunner) Run(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(context.Background(), session, prompt)
}

func (r liveSkillSelectorRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	session.AddSystemContext(prompt)
	session.AddAssistantMessage(r.output)
	return engine.Result{Output: r.output, Session: session}, nil
}

type contextAwareLiveSkillSelectorRunner struct {
	prompt *string
}

func (r contextAwareLiveSkillSelectorRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r contextAwareLiveSkillSelectorRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	if r.prompt != nil {
		*r.prompt = prompt
	}
	output := `{"action":"none","skill":"","args":"","confidence":0.0,"reason":"missing context"}`
	if strings.Contains(prompt, "帮我生成一张狗的图片") && strings.Contains(prompt, "你自己决定") {
		output = `{"action":"skill_call","skill":"vertex-image-artifact","args":"一张狗的图片，风格由系统决定","confidence":0.9,"reason":"latest utterance continues the image request"}`
	}
	session.AddSystemContext(prompt)
	session.AddAssistantMessage(output)
	return engine.Result{Output: output, Session: session}, nil
}

func TestRuntimeExecuteLiveSkillCommandRunsSkill(t *testing.T) {
	root := t.TempDir()
	store := NewFileSessionStore(root)
	catalog := fakeSkillCatalog{skills: []*skills.SkillDefinition{
		{
			Name:          "diagram",
			DisplayName:   "架构图",
			UserInvocable: true,
			GetPrompt: func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
				return []skills.ContentBlock{{Type: "text", Text: "diagram prompt: " + args}}, nil
			},
		},
	}}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, store, nil, catalog, func(Scope) Runner { return echoRunner{} })
	session, err := runtime.CreateSession(context.Background(), "alice", root)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sink := &collectSink{}

	handled, err := runtime.ExecuteLiveSkillCommand(context.Background(), "alice", session.ID, "使用 架构图 技能画一个登录流程", sink)
	if err != nil {
		t.Fatalf("ExecuteLiveSkillCommand: %v", err)
	}
	if !handled {
		t.Fatal("expected live skill command to be handled")
	}
	var sawStart, sawResult, sawAssistant bool
	for _, event := range sink.events {
		switch event.Type {
		case "live_skill_start":
			sawStart = true
		case "live_skill_result":
			sawResult = true
		case "message":
			if event.Role == state.MessageRoleAssistant && strings.Contains(event.Content, "diagram prompt: 画一个登录流程") {
				sawAssistant = true
			}
		}
	}
	if !sawStart || !sawResult || !sawAssistant {
		t.Fatalf("missing live skill events start=%t result=%t assistant=%t events=%#v", sawStart, sawResult, sawAssistant, sink.events)
	}
	saved, err := store.Get(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	foundOriginalUtterance := false
	for _, message := range saved.Messages {
		if !message.Hidden && message.Role == state.MessageRoleUser && strings.Contains(message.Content, "架构图") {
			foundOriginalUtterance = true
			break
		}
	}
	if !foundOriginalUtterance {
		t.Fatalf("expected original live utterance to be persisted, got %#v", saved.Messages)
	}
}

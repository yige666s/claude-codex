package agentruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %#v", len(events), events)
	}
	if events[0].Type != "live_transcript" || events[0].Role != state.MessageRoleUser || events[0].Content != "你好" {
		t.Fatalf("unexpected input transcript event: %#v", events[0])
	}
	if events[2].Type != "live_audio" {
		t.Fatalf("unexpected audio event: %#v", events[2])
	}
	var audio map[string]string
	if err := json.Unmarshal(events[2].Data, &audio); err != nil {
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
	if len(events) != 1 || events[0].Type != "live_audio" {
		t.Fatalf("unexpected events: %#v", events)
	}
	var audio map[string]string
	if err := json.Unmarshal(events[0].Data, &audio); err != nil {
		t.Fatalf("decode audio payload: %v", err)
	}
	if audio["mime_type"] != "audio/L16;rate=24000" || audio["data"] != "AQID" {
		t.Fatalf("unexpected audio payload: %#v", audio)
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

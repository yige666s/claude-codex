package agentruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestMemoryRecallDeciderUsesEmbeddingDriftLayer(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("我们正在讨论 Redis SSH 连接问题")
	session.AddAssistantMessage("目前看起来是 SSH 私钥和用户不匹配。")
	session.AddUserMessage("那我先试一下")
	session.AddAssistantMessage("可以，试完告诉我结果。")
	session.AddUserMessage("换个话题，帮我规划东京旅行")

	decider := NewMemoryRecallDecider(MemoryRecallConfig{
		Configured:                   true,
		Enabled:                      true,
		ConditionalEnabled:           true,
		AsyncEnabled:                 false,
		Timeout:                      time.Second,
		RecentContextMessages:        2,
		RecentContextMaxRunes:        400,
		ForceInterval:                0,
		ComplexTokenThreshold:        200,
		EmbeddingEnabled:             true,
		EmbeddingSimilarityThreshold: 0.75,
		EmbeddingWindow:              2,
	}, staticEmbeddingRecallEmbedder{
		"换个话题，帮我规划东京旅行":                   {1, 0},
		"assistant: 目前看起来是 SSH 私钥和用户不匹配。": {0, 1},
		"user: 我们正在讨论 Redis SSH 连接问题":     {0, 1},
	}, nil)

	decision := decider.Decide(context.Background(), MemoryRecallInput{Session: session, Message: "换个话题，帮我规划东京旅行"})
	if !decision.Should || decision.Reason != memoryRecallReasonEmbeddingDrift {
		t.Fatalf("expected embedding drift recall, got %#v", decision)
	}
}

func TestMemoryRecallDeciderSkipsSimilarEmbeddingContinuation(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("我们正在讨论 Redis SSH 连接问题")
	session.AddAssistantMessage("目前看起来是 SSH 私钥和用户不匹配。")
	session.AddUserMessage("那私钥应该怎么换")

	decider := NewMemoryRecallDecider(MemoryRecallConfig{
		Configured:                   true,
		Enabled:                      true,
		ConditionalEnabled:           true,
		AsyncEnabled:                 false,
		Timeout:                      time.Second,
		RecentContextMessages:        2,
		RecentContextMaxRunes:        400,
		ForceInterval:                0,
		ComplexTokenThreshold:        200,
		EmbeddingEnabled:             true,
		EmbeddingSimilarityThreshold: 0.75,
		EmbeddingWindow:              2,
		IntentClassifierEnabled:      true,
		IntentClassifierThreshold:    0.6,
		IntentClassifierContextTurns: 2,
	}, staticEmbeddingRecallEmbedder{
		"那私钥应该怎么换":                                                                   {0, 1},
		"assistant: 目前看起来是 SSH 私钥和用户不匹配。":                                            {0, 1},
		"user: 我们正在讨论 Redis SSH 连接问题":                                                {0, 1},
		buildMemoryRecallIntentInput(session, "那私钥应该怎么换", memoryRecallTestConfig(2)): {0, 1},
		memoryRecallIntentText("new_topic"):                                          {1, 0},
		memoryRecallIntentText("continuation"):                                       {0, 1},
		memoryRecallIntentText("history_ref"):                                        {1, 0},
		memoryRecallIntentText("chit_chat"):                                          {0, 1},
	}, nil)

	decision := decider.Decide(context.Background(), MemoryRecallInput{Session: session, Message: "那私钥应该怎么换"})
	if decision.Should {
		t.Fatalf("expected similar continuation to skip recall, got %#v", decision)
	}
	if decision.Reason != memoryRecallReasonNoRecall {
		t.Fatalf("expected no_recall_needed, got %#v", decision)
	}
}

func TestMemoryRecallDeciderUsesIntentClassifierLayer(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("我们正在讨论部署配置")
	session.AddAssistantMessage("当前配置是按照默认部署习惯整理的。")
	session.AddUserMessage("那这会影响我的默认部署习惯吗")
	config := memoryRecallTestConfig(2)
	config.IntentClassifierEnabled = true
	config.IntentClassifierThreshold = 0.6

	decider := NewMemoryRecallDecider(config, staticEmbeddingRecallEmbedder{
		"那这会影响我的默认部署习惯吗":                                                {0, 1},
		"assistant: 当前配置是按照默认部署习惯整理的。":                                  {0, 1},
		"user: 我们正在讨论部署配置":                                              {0, 1},
		buildMemoryRecallIntentInput(session, "那这会影响我的默认部署习惯吗", config): {1, 0},
		memoryRecallIntentText("new_topic"):                             {0, 1},
		memoryRecallIntentText("continuation"):                          {0, 1},
		memoryRecallIntentText("history_ref"):                           {1, 0},
		memoryRecallIntentText("chit_chat"):                             {0, 1},
	}, nil)

	decision := decider.Decide(context.Background(), MemoryRecallInput{Session: session, Message: "那这会影响我的默认部署习惯吗"})
	if !decision.Should || decision.Reason != memoryRecallReasonIntentClassifier {
		t.Fatalf("expected intent classifier recall, got %#v", decision)
	}
}

func TestMemoryRecallDeciderFallsBackWhenEmbeddingFails(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("我们正在讨论 Redis SSH 连接问题")
	session.AddAssistantMessage("目前看起来是 SSH 私钥和用户不匹配。")
	session.AddUserMessage("那我先试一下")
	session.AddAssistantMessage("可以，试完告诉我结果。")

	decider := NewMemoryRecallDecider(MemoryRecallConfig{
		Configured:                   true,
		Enabled:                      true,
		ConditionalEnabled:           true,
		AsyncEnabled:                 false,
		Timeout:                      time.Second,
		RecentContextMessages:        2,
		RecentContextMaxRunes:        400,
		ForceInterval:                0,
		ComplexTokenThreshold:        200,
		EmbeddingEnabled:             true,
		EmbeddingSimilarityThreshold: 0.75,
		EmbeddingWindow:              2,
		MinQueryRunes:                8,
	}, failingEmbeddingRecallEmbedder{}, nil)

	decision := decider.Decide(context.Background(), MemoryRecallInput{Session: session, Message: "帮我重新检查一下服务器数据库连接"})
	if !decision.Should || decision.Reason != memoryRecallReasonEmbeddingUnavailable {
		t.Fatalf("expected embedding failure fallback recall, got %#v", decision)
	}
}

func memoryRecallTestConfig(contextTurns int) MemoryRecallConfig {
	return MemoryRecallConfig{
		Configured:                   true,
		Enabled:                      true,
		ConditionalEnabled:           true,
		AsyncEnabled:                 false,
		Timeout:                      time.Second,
		RecentContextMessages:        contextTurns,
		RecentContextMaxRunes:        400,
		ForceInterval:                0,
		ComplexTokenThreshold:        200,
		EmbeddingEnabled:             true,
		EmbeddingSimilarityThreshold: 0.75,
		EmbeddingWindow:              contextTurns,
		MinQueryRunes:                8,
		IntentClassifierContextTurns: contextTurns,
	}
}

func memoryRecallIntentText(name string) string {
	for _, label := range memoryRecallIntentLabels {
		if label.name == name {
			return label.text
		}
	}
	return name
}

type staticEmbeddingRecallEmbedder map[string][]float32

func (e staticEmbeddingRecallEmbedder) EmbedQuery(_ context.Context, query string) ([]float32, error) {
	if vector, ok := e[query]; ok {
		return vector, nil
	}
	return []float32{1, 1}, nil
}

type failingEmbeddingRecallEmbedder struct{}

func (failingEmbeddingRecallEmbedder) EmbedQuery(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedding unavailable")
}

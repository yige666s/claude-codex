package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestMemoryBM25ScoresFavorRareTerms(t *testing.T) {
	documents := []string{
		"redis cache latency common",
		"redis cache export common",
		"redis cache backup common",
		"redis cache tunnel common",
		"redis cache project common",
		"navicat tunnel private common",
	}

	scores := memoryBM25Scores("redis navicat", documents)
	if len(scores) != len(documents) {
		t.Fatalf("expected %d BM25 scores, got %d", len(documents), len(scores))
	}
	if scores[5] <= scores[0] {
		t.Fatalf("expected rare navicat match to outrank common redis match, got scores=%#v", scores)
	}
	if scores[5] != 1 {
		t.Fatalf("expected normalized top BM25 score to be 1, got scores=%#v", scores)
	}
}

func TestMemoryBM25ScoresMatchCJKBigrams(t *testing.T) {
	scores := memoryBM25Scores("北京周边", []string{
		"用户住在北京，偏好周末短途安排",
		"用户喜欢结构化输出",
	})
	if scores[0] <= scores[1] || scores[0] == 0 {
		t.Fatalf("expected CJK bigram BM25 match for 北京周边, got scores=%#v", scores)
	}
}

func TestFileMemoryServiceLoadContextUsesBM25KeywordRanking(t *testing.T) {
	ctx := context.Background()
	memory := NewFileMemoryService(t.TempDir())
	now := time.Now().UTC().Add(-time.Hour)
	rare := newConversationMemoryItem("alice", "", "Navicat SSH tunnel uses a private key for production access")
	rare.ID = "memory-navicat"
	rare.CreatedAt = now
	rare.UpdatedAt = now
	if _, err := memory.UpdateMemoryItem(ctx, "alice", rare); err != nil {
		t.Fatalf("update rare memory: %v", err)
	}
	for i := 0; i < 6; i++ {
		item := newConversationMemoryItem("alice", "", fmt.Sprintf("Redis cache note %d covers routine export and backup work", i))
		item.ID = fmt.Sprintf("memory-redis-%d", i)
		item.CreatedAt = now
		item.UpdatedAt = now
		if _, err := memory.UpdateMemoryItem(ctx, "alice", item); err != nil {
			t.Fatalf("update redis memory %d: %v", i, err)
		}
	}
	session := state.NewSession(t.TempDir())
	session.ID = "session-bm25"
	session.AddUserMessage("redis navicat")

	contextText, err := memory.LoadContext(ctx, "alice", session)
	if err != nil {
		t.Fatalf("load memory context: %v", err)
	}
	firstLine := firstMemoryBullet(contextText)
	if !strings.Contains(firstLine, "Navicat SSH tunnel") {
		t.Fatalf("expected BM25-ranked rare memory first, got first=%q context=%q", firstLine, contextText)
	}
}

func TestMemoryExtractorCapturesChineseResidenceMoveFact(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("我现在搬到北京市海淀区居住了")
	session.AddAssistantMessage("已更新")

	candidates, err := NewRuleMemoryExtractor().Extract(context.Background(), MemoryExtractionInput{
		UserID:    "alice",
		SessionID: session.ID,
		Messages:  session.Messages,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("extract memory candidates: %v", err)
	}
	items := evaluateMemoryCandidates("alice", session.ID, candidates)
	for _, item := range items {
		if item.Category == MemoryCategoryFact && strings.Contains(item.Content, "海淀区") {
			return
		}
	}
	t.Fatalf("expected Chinese residence move fact memory, got %#v", items)
}

func TestFileMemoryServiceArchivesOldChineseResidenceOnMove(t *testing.T) {
	ctx := context.Background()
	memory := NewFileMemoryService(t.TempDir())
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("我居住在北京市通州区")
	session.AddAssistantMessage("好的")
	if err := memory.AfterTurn(ctx, "alice", session); err != nil {
		t.Fatalf("first memory turn: %v", err)
	}

	session.AddUserMessage("我现在搬到北京市海淀区居住了")
	session.AddAssistantMessage("已更新")
	if err := memory.AfterTurn(ctx, "alice", session); err != nil {
		t.Fatalf("move memory turn: %v", err)
	}

	items, err := memory.ListMemoryItems(ctx, "alice", MemoryItemFilter{})
	if err != nil {
		t.Fatalf("list memory: %v", err)
	}
	var activeNew, archivedOld MemoryItem
	for _, item := range items {
		if strings.Contains(item.Content, "海淀区") && item.Status == MemoryStatusActive {
			activeNew = item
		}
		if strings.Contains(item.Content, "通州区") && item.Status == MemoryStatusArchived {
			archivedOld = item
		}
	}
	if activeNew.ID == "" || archivedOld.ID == "" || archivedOld.SupersededByID != activeNew.ID {
		t.Fatalf("expected move to archive old residence and keep new active, got %#v", items)
	}
}

func firstMemoryBullet(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- [") {
			return line
		}
	}
	return ""
}

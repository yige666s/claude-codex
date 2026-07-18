package agentruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestLoadMemoryPolicyFileOverridesExtractionRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-policy.yaml")
	data := []byte(`
version: custom-memory-v1
extraction:
  rules:
    - id: fact-pet
      kind: fact
      pattern: '(?i)\bmy\s+pet\s+is\s+([^\n。.!?]+)'
      category: fact
      tags: [fact, pet]
      confidence: 0.81
      importance: 0.72
      reason: pet_fact
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	policy, err := LoadMemoryPolicyFile(path, "custom-memory-v1")
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("my pet is Pixel")
	candidates, err := NewRuleMemoryExtractorWithPolicy(policy).Extract(context.Background(), MemoryExtractionInput{
		UserID:    "alice",
		SessionID: session.ID,
		Messages:  session.Messages,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	items := evaluateMemoryCandidatesWithPolicy("alice", session.ID, candidates, policy)
	if len(items) != 1 {
		t.Fatalf("expected one custom policy memory, got %#v", items)
	}
	if !strings.Contains(items[0].Content, "Pixel") {
		t.Fatalf("expected custom extraction content, got %#v", items[0])
	}
	if items[0].Metadata["memory_policy_version"] != "custom-memory-v1" || items[0].Metadata["extraction_rule_id"] != "fact-pet" {
		t.Fatalf("expected policy metadata, got %#v", items[0].Metadata)
	}
}

func TestMemoryPolicyOverridesRecallQueryExpansion(t *testing.T) {
	policy := DefaultMemoryPolicy()
	policy.Version = "custom-recall-v1"
	policy.Recall.QueryPreamblePhrases = []string{"查查"}
	policy.Recall.Expansions = []MemoryRecallExpansion{{
		ID:        "pet",
		Triggers:  []string{"宠物"},
		Expansion: "user pet profile saved care preferences",
	}}
	rewriter := NewDeterministicMemoryQueryRewriterWithPolicy(policy)
	got, err := rewriter.RewriteMemoryRecallQuery(context.Background(), MemoryQueryRewriteInput{
		OriginalQuery: "查查宠物安排",
		Config:        defaultMemoryRecallConfig(),
	})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !got.Used || !strings.Contains(got.Query, "宠物安排") || !strings.Contains(got.Query, "user pet profile") {
		t.Fatalf("expected custom policy rewrite, got %#v", got)
	}
}

func TestMemoryPolicyConflictSlotArchivesOldCurrentValue(t *testing.T) {
	policy := DefaultMemoryPolicy()
	policy.Version = "custom-conflict-v1"
	policy.Conflict.Slots = []MemoryConflictSlotPolicy{{
		ID:      "favorite-color",
		Name:    "favorite_color",
		Markers: []string{"favorite color is "},
	}}
	existing := []MemoryItem{
		newConversationMemoryItem("alice", "session", "favorite color is blue"),
	}
	existing[0].ID = "old-color"
	candidate := newConversationMemoryItem("alice", "session", "current favorite color is red")
	candidate.ID = "new-color"

	got, updates := applyMemoryConflictResolutionWithPolicy(existing, candidate, policy)
	if len(updates) != 1 || updates[0].Status != MemoryStatusArchived {
		t.Fatalf("expected old value archived, got candidate=%#v updates=%#v", got, updates)
	}
	if updates[0].Metadata["conflict_rule_id"] != "favorite-color" || got.Metadata["conflict_rule_id"] != "favorite-color" {
		t.Fatalf("expected conflict rule metadata, got candidate=%#v update=%#v", got.Metadata, updates[0].Metadata)
	}
}

func TestMemoryPolicyFileReloaderAutomaticallyAppliesChangedPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-policy.json")
	writeMemoryPolicyRuleFile(t, path, "hot-v1", "fact-pet", `(?i)\bmy\s+pet\s+is\s+([^\n。.!?]+)`)
	reloader, err := NewMemoryPolicyFileReloader(MemoryPolicyFileReloaderConfig{
		Path:           path,
		ReloadInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new reloader: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- reloader.Run(ctx)
	}()
	extractor := NewRuleMemoryExtractorWithProvider(reloader)
	assertExtractorCapturesPolicyRule(t, extractor, reloader.MemoryPolicy(), "my pet is Pixel", "Pixel")

	writeMemoryPolicyRuleFile(t, path, "hot-v2", "fact-vehicle", `(?i)\bmy\s+vehicle\s+is\s+([^\n。.!?]+)`)
	waitForMemoryPolicyVersion(t, reloader, "hot-v2")
	assertExtractorCapturesPolicyRule(t, extractor, reloader.MemoryPolicy(), "my vehicle is Roadster", "Roadster")

	cancel()
	if err := <-errCh; err != nil && err != context.Canceled {
		t.Fatalf("reloader run: %v", err)
	}
}

func TestMemoryPolicyFileReloaderKeepsLastKnownGoodPolicyOnInvalidChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-policy.json")
	writeMemoryPolicyRuleFile(t, path, "hot-v1", "fact-pet", `(?i)\bmy\s+pet\s+is\s+([^\n。.!?]+)`)
	reloader, err := NewMemoryPolicyFileReloader(MemoryPolicyFileReloaderConfig{Path: path})
	if err != nil {
		t.Fatalf("new reloader: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":"bad","extraction":{"rules":[{"id":"bad","kind":"fact","pattern":"["}]}}`), 0o644); err != nil {
		t.Fatalf("write invalid policy: %v", err)
	}
	if changed, err := reloader.ReloadIfChanged(context.Background()); err == nil || changed {
		t.Fatalf("expected invalid reload to fail without applying, changed=%v err=%v", changed, err)
	}
	if got := reloader.MemoryPolicy().Version; got != "hot-v1" {
		t.Fatalf("expected last known good version hot-v1, got %q", got)
	}
}

func TestDefaultMemoryPolicySmokeEvalPasses(t *testing.T) {
	report := EvaluateMemoryPolicySmoke(DefaultMemoryPolicy())
	if !report.Passed {
		t.Fatalf("expected default memory policy smoke eval to pass, got %#v", report)
	}
}

func writeMemoryPolicyRuleFile(t *testing.T, path, version, ruleID, pattern string) {
	t.Helper()
	payload := fmt.Sprintf(`{
  "version": %q,
  "extraction": {
    "rules": [{
      "id": %q,
      "kind": "fact",
      "pattern": %q,
      "category": "fact",
      "tags": ["fact"],
      "confidence": 0.81,
      "importance": 0.72,
      "reason": "test_policy_rule"
    }]
  }
}`, version, ruleID, pattern)
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}

func waitForMemoryPolicyVersion(t *testing.T, provider MemoryPolicyProvider, version string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if provider.MemoryPolicy().Version == version {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for memory policy version %q, got %q", version, provider.MemoryPolicy().Version)
}

func assertExtractorCapturesPolicyRule(t *testing.T, extractor MemoryExtractor, policy MemoryPolicy, message, want string) {
	t.Helper()
	session := state.NewSession(t.TempDir())
	session.AddUserMessage(message)
	candidates, err := extractor.Extract(context.Background(), MemoryExtractionInput{
		UserID:    "alice",
		SessionID: session.ID,
		Messages:  session.Messages,
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	items := evaluateMemoryCandidatesWithPolicy("alice", session.ID, candidates, policy)
	for _, item := range items {
		if strings.Contains(item.Content, want) {
			return
		}
	}
	t.Fatalf("expected extracted memory containing %q, got %#v", want, items)
}

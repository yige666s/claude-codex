package engine

import (
	"context"
	"strings"
	"testing"

	"claude-codex/internal/harness/messages"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

func TestSkillListingManagerIncremental(t *testing.T) {
	manager := messages.NewSkillListingManager()

	skill1 := &skills.SkillDefinition{
		Name:        "skill1",
		Description: "First skill",
		Source:      skills.SourceBundled,
	}

	skill2 := &skills.SkillDefinition{
		Name:        "skill2",
		Description: "Second skill",
		Source:      skills.SourceBundled,
	}

	// First call should return skill1
	attachment1 := manager.GetSkillListingAttachment([]*skills.SkillDefinition{skill1}, 200000)
	if attachment1 == nil {
		t.Fatal("First attachment should not be nil")
	}
	if !attachment1.IsInitial {
		t.Error("First attachment should be marked as initial")
	}
	if attachment1.SkillCount != 1 {
		t.Errorf("Expected 1 skill, got %d", attachment1.SkillCount)
	}

	// Second call with same skill should return nil (already sent)
	attachment2 := manager.GetSkillListingAttachment([]*skills.SkillDefinition{skill1}, 200000)
	if attachment2 != nil {
		t.Error("Second attachment should be nil (skill already sent)")
	}

	// Third call with new skill should return only skill2
	attachment3 := manager.GetSkillListingAttachment([]*skills.SkillDefinition{skill1, skill2}, 200000)
	if attachment3 == nil {
		t.Fatal("Third attachment should not be nil")
	}
	if attachment3.IsInitial {
		t.Error("Third attachment should not be marked as initial")
	}
	if attachment3.SkillCount != 1 {
		t.Errorf("Expected 1 new skill, got %d", attachment3.SkillCount)
	}
}

func TestStreamingRunInjectsFullSkillDescriptionsAfterHiddenContext(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddSystemContext("<consumer-security>already injected</consumer-security>")
	fullDescription := "Generate images. Triggers include: 生成以下图片, unique-image-trigger-marker"
	manager := skills.NewSkillManager()
	if err := manager.RegisterLoadedSkills([]*skills.SkillDefinition{{
		Name:          "vertex-image-artifact",
		Description:   fullDescription,
		UserInvocable: true,
	}}); err != nil {
		t.Fatalf("register skill: %v", err)
	}
	planner := &capturingStreamingPlanner{}
	engine := NewWithDir(planner, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	engine.SetSkillManager(manager)
	if _, err := engine.RunStream(context.Background(), session, "帮我生成以下图片：Cute little kitty", nil); err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if !messagesContain(planner.messages, "unique-image-trigger-marker") {
		t.Fatalf("expected full skill description in streaming planner context, got %#v", planner.messages)
	}
	if !messagesContain(planner.messages, "帮我生成以下图片：Cute little kitty") {
		t.Fatalf("expected user prompt in streaming planner context, got %#v", planner.messages)
	}
	if session.Metadata[skillCatalogInjectedKey] != "true" {
		t.Fatalf("skill catalog metadata not marked: %#v", session.Metadata)
	}
	for _, message := range session.Messages {
		if message.Role == "assistant" && message.Content == "Understood. I have the workspace context." && !message.Hidden {
			t.Fatalf("workspace context acknowledgement should be hidden: %#v", message)
		}
	}
}

type capturingStreamingPlanner struct {
	messages []state.Message
}

func (p *capturingStreamingPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (Plan, error) {
	return Plan{AssistantText: "ok", StopReason: "end_turn"}, nil
}

func (p *capturingStreamingPlanner) StreamNext(_ context.Context, session *state.Session, _ []toolkit.Descriptor, _ func(string)) (Plan, error) {
	p.messages = append([]state.Message(nil), session.Messages...)
	return Plan{AssistantText: "ok", StopReason: "end_turn"}, nil
}

func messagesContain(items []state.Message, needle string) bool {
	for _, item := range items {
		if strings.Contains(item.Content, needle) {
			return true
		}
	}
	return false
}

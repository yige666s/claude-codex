package engine

import (
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/messages"
	"github.com/ding/claude-code/claude-go/internal/harness/skills"
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

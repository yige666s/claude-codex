package messages

import (
	"encoding/json"
	"fmt"
)

// AttachmentType represents the type of attachment
type AttachmentType string

const (
	AttachmentTypeSkillListing AttachmentType = "skill_listing"
	// Add other attachment types as needed
)

const SkillSelectionPolicyVersion = "2"

func SkillSelectionPolicyMarker() string {
	return fmt.Sprintf(`skill-selection-policy version="%s"`, SkillSelectionPolicyVersion)
}

// Attachment represents a message attachment
type Attachment interface {
	Type() AttachmentType
	ToSystemReminder() string
}

// SkillListingAttachment represents a skill listing attachment
type SkillListingAttachment struct {
	Content    string `json:"content"`
	SkillCount int    `json:"skillCount"`
	IsInitial  bool   `json:"isInitial"`
}

func (a *SkillListingAttachment) Type() AttachmentType {
	return AttachmentTypeSkillListing
}

func (a *SkillListingAttachment) ToSystemReminder() string {
	if a.Content == "" {
		return ""
	}
	return fmt.Sprintf(`<system-reminder>
The following product skills are available through the Skill tool:

%s

<skill-selection-policy version="%s">
Skill selection rules:
- If the current user request matches one of these skills, you MUST call the Skill tool before responding.
- Do not merely promise that a skill task is starting or will be done later.
- For document, image, artifact, or job-style deliverables, call the Skill tool and let the runtime route the work.
- Pass the user's actual task/request as the Skill args, preserving uploaded attachment context already shown in the conversation.
- If no listed skill matches, answer normally without inventing a skill.
</skill-selection-policy>
</system-reminder>`, a.Content, SkillSelectionPolicyVersion)
}

// AttachmentMessage represents a message with an attachment
type AttachmentMessage struct {
	Attachment Attachment
	IsMeta     bool
}

func (m *AttachmentMessage) ToSystemReminder() string {
	if m.Attachment == nil {
		return ""
	}
	return m.Attachment.ToSystemReminder()
}

// MarshalJSON implements json.Marshaler
func (a *SkillListingAttachment) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type       AttachmentType `json:"type"`
		Content    string         `json:"content"`
		SkillCount int            `json:"skillCount"`
		IsInitial  bool           `json:"isInitial"`
	}{
		Type:       a.Type(),
		Content:    a.Content,
		SkillCount: a.SkillCount,
		IsInitial:  a.IsInitial,
	})
}

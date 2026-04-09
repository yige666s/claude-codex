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
	return fmt.Sprintf("<system-reminder>\nThe following skills are available for use with the Skill tool:\n\n%s\n</system-reminder>", a.Content)
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

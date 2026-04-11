package input

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/public/types"
)

// AttachmentType represents the type of attachment.
type AttachmentType string

const (
	AttachmentTypeFile         AttachmentType = "file"
	AttachmentTypeDirectory    AttachmentType = "directory"
	AttachmentTypeIDESelection AttachmentType = "ide_selection"
	AttachmentTypeAgentMention AttachmentType = "agent_mention"
	AttachmentTypeTask         AttachmentType = "task"
	AttachmentTypePlan         AttachmentType = "plan"
)

// Attachment represents an attachment to a message.
type Attachment struct {
	Type AttachmentType
	Data map[string]interface{}
}

// AttachmentMessage represents a message with an attachment.
type AttachmentMessage struct {
	UUID       string
	Attachment Attachment
	Content    []types.ContentBlock
}

// ProcessAttachments processes attachments and returns attachment messages.
func ProcessAttachments(opts *ProcessUserInputOptions) ([]AttachmentMessage, error) {
	if opts.SkipAttachments {
		return nil, nil
	}

	var attachmentMessages []AttachmentMessage

	// Process IDE selection
	if opts.Context != nil && opts.Context.IDESelection != nil {
		msg, err := createIDESelectionAttachment(opts.Context.IDESelection)
		if err != nil {
			return nil, fmt.Errorf("failed to create IDE selection attachment: %w", err)
		}
		if msg != nil {
			attachmentMessages = append(attachmentMessages, *msg)
		}
	}

	return attachmentMessages, nil
}

// createIDESelectionAttachment creates an attachment message for IDE selection.
func createIDESelectionAttachment(selection *IDESelection) (*AttachmentMessage, error) {
	if selection == nil || selection.FilePath == "" {
		return nil, nil
	}

	// Read file content if not provided
	content := selection.Content
	if content == "" && selection.FilePath != "" {
		data, err := os.ReadFile(selection.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", selection.FilePath, err)
		}
		content = string(data)
	}

	// Extract selected lines if specified
	if selection.StartLine > 0 && selection.EndLine > 0 {
		lines := strings.Split(content, "\n")
		if selection.StartLine <= len(lines) && selection.EndLine <= len(lines) {
			selectedLines := lines[selection.StartLine-1 : selection.EndLine]
			content = strings.Join(selectedLines, "\n")
		}
	}

	// Create attachment message
	msg := &AttachmentMessage{
		Attachment: Attachment{
			Type: AttachmentTypeIDESelection,
			Data: map[string]interface{}{
				"filePath":  selection.FilePath,
				"startLine": selection.StartLine,
				"endLine":   selection.EndLine,
			},
		},
		Content: []types.ContentBlock{
			{
				Type: "text",
				Text: fmt.Sprintf("<ide_selection>\nFile: %s\nLines: %d-%d\n\n%s\n</ide_selection>",
					selection.FilePath,
					selection.StartLine,
					selection.EndLine,
					content,
				),
			},
		},
	}

	return msg, nil
}

// CreateFileAttachment creates an attachment for a file.
func CreateFileAttachment(filePath string) (*AttachmentMessage, error) {
	// Expand path
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	attachmentType := AttachmentTypeFile
	if info.IsDir() {
		attachmentType = AttachmentTypeDirectory
	}

	// Read file content (if not too large)
	var content string
	if !info.IsDir() && info.Size() < 1024*1024 { // 1MB limit
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
	}

	msg := &AttachmentMessage{
		Attachment: Attachment{
			Type: attachmentType,
			Data: map[string]interface{}{
				"filePath": absPath,
				"size":     info.Size(),
				"isDir":    info.IsDir(),
			},
		},
		Content: []types.ContentBlock{
			{
				Type: "text",
				Text: fmt.Sprintf("<file_attachment>\nPath: %s\nSize: %d bytes\n\n%s\n</file_attachment>",
					absPath,
					info.Size(),
					content,
				),
			},
		},
	}

	return msg, nil
}

// CreateAgentMentionAttachment creates an attachment for agent mention.
func CreateAgentMentionAttachment(agentType string) *AttachmentMessage {
	return &AttachmentMessage{
		Attachment: Attachment{
			Type: AttachmentTypeAgentMention,
			Data: map[string]interface{}{
				"agentType": agentType,
			},
		},
		Content: []types.ContentBlock{
			{
				Type: "text",
				Text: fmt.Sprintf("<agent_mention>@agent-%s</agent_mention>", agentType),
			},
		},
	}
}

// ParseAttachmentReferences parses attachment references from input string.
func ParseAttachmentReferences(input string) []string {
	var refs []string

	// Look for @file:path patterns
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "@file:") {
			path := strings.TrimPrefix(strings.TrimSpace(line), "@file:")
			refs = append(refs, path)
		}
	}

	return refs
}

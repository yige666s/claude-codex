package input

import (
	"context"
	"fmt"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// ProcessUserInputContext contains the context needed for processing user input.
type ProcessUserInputContext struct {
	// WorkingDir is the current working directory
	WorkingDir string

	// SessionID is the current session identifier
	SessionID string

	// PermissionMode is the current permission mode
	PermissionMode string

	// Messages are the existing conversation messages
	Messages []types.Message

	// IDESelection contains IDE selection information
	IDESelection *IDESelection

	// PastedContents contains pasted content by ID
	PastedContents map[int]PastedContent
}

// IDESelection represents IDE selection information.
type IDESelection struct {
	// FilePath is the selected file path
	FilePath string

	// StartLine is the start line of selection
	StartLine int

	// EndLine is the end line of selection
	EndLine int

	// Content is the selected content
	Content string
}

// PastedContent represents pasted content.
type PastedContent struct {
	// Type is the content type (text, image, etc.)
	Type string

	// Content is the actual content
	Content string

	// Data is additional data (e.g., base64 image data)
	Data []byte
}

// ProcessUserInputResult contains the result of processing user input.
type ProcessUserInputResult struct {
	// Messages are the messages to add to the conversation
	Messages []types.Message

	// ShouldQuery indicates whether to proceed with the query
	ShouldQuery bool

	// AllowedTools is the list of tools allowed for this query
	AllowedTools []string

	// Model is the model to use (if overridden)
	Model string

	// ResultText is output text for non-interactive mode
	ResultText string

	// NextInput is the next input to submit (for command chaining)
	NextInput string

	// SubmitNextInput indicates whether to auto-submit the next input
	SubmitNextInput bool
}

// ProcessUserInputOptions contains options for processing user input.
type ProcessUserInputOptions struct {
	// Input is the user input string or content blocks
	Input any // string or []types.ContentBlock

	// PreExpansionInput is input before [Pasted text #N] expansion
	PreExpansionInput string

	// Mode is the input mode ("prompt", "edit", etc.)
	Mode string

	// Context is the processing context
	Context *ProcessUserInputContext

	// UUID is the message UUID
	UUID string

	// QuerySource indicates where the query came from
	QuerySource string

	// SkipSlashCommands treats "/" as plain text
	SkipSlashCommands bool

	// BridgeOrigin indicates if from bridge/CCR
	BridgeOrigin bool

	// IsMeta indicates this is a system-generated prompt
	IsMeta bool

	// SkipAttachments skips attachment processing
	SkipAttachments bool

	// IsAlreadyProcessing indicates if already processing
	IsAlreadyProcessing bool
}

// ProcessUserInput processes user input and returns messages to add to the conversation.
// This is the main entry point for handling user input.
func ProcessUserInput(ctx context.Context, opts *ProcessUserInputOptions) (*ProcessUserInputResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("options cannot be nil")
	}

	// Convert input to string
	inputString, err := getInputString(opts.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to get input string: %w", err)
	}

	// Process attachments
	attachmentMessages, err := ProcessAttachments(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to process attachments: %w", err)
	}

	// Process images from pasted contents
	var imageBlocks []types.ContentBlock
	var imagePasteIDs []int
	if opts.Context != nil && opts.Context.PastedContents != nil {
		imageBlocks, imagePasteIDs, err = ProcessImageContent(opts.Context.PastedContents)
		if err != nil {
			return nil, fmt.Errorf("failed to process image content: %w", err)
		}
	}

	// Check for slash commands
	if !opts.SkipSlashCommands && strings.HasPrefix(inputString, "/") {
		// Check if bridge-safe command when from bridge
		if opts.BridgeOrigin {
			// TODO: Implement bridge-safe command check
			// For now, skip slash command processing for bridge origin
			return processRegularInput(ctx, inputString, opts, imageBlocks, imagePasteIDs, attachmentMessages)
		}
		return processSlashCommand(ctx, inputString, opts, imageBlocks, imagePasteIDs, attachmentMessages)
	}

	// Process as regular user input
	return processRegularInput(ctx, inputString, opts, imageBlocks, imagePasteIDs, attachmentMessages)
}

// getInputString converts input to string.
func getInputString(input any) (string, error) {
	switch v := input.(type) {
	case string:
		return v, nil
	case []types.ContentBlock:
		// Extract text from content blocks
		var parts []string
		for _, block := range v {
			if block.Type == "text" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n"), nil
	default:
		return "", fmt.Errorf("unsupported input type: %T", input)
	}
}

// processSlashCommand processes a slash command.
func processSlashCommand(ctx context.Context, input string, opts *ProcessUserInputOptions, imageBlocks []types.ContentBlock, imagePasteIDs []int, attachmentMessages []AttachmentMessage) (*ProcessUserInputResult, error) {
	// Parse slash command
	cmd, args := parseSlashCommand(input)

	// TODO: Implement slash command execution
	// For now, treat as regular input
	_ = cmd
	_ = args

	return processRegularInput(ctx, input, opts, imageBlocks, imagePasteIDs, attachmentMessages)
}

// parseSlashCommand parses a slash command into command and arguments.
func parseSlashCommand(input string) (command string, args string) {
	input = strings.TrimPrefix(input, "/")
	parts := strings.SplitN(input, " ", 2)

	command = parts[0]
	if len(parts) > 1 {
		args = parts[1]
	}

	return command, args
}

// processRegularInput processes regular user input (non-slash command).
func processRegularInput(_ context.Context, input string, opts *ProcessUserInputOptions, imageBlocks []types.ContentBlock, imagePasteIDs []int, attachmentMessages []AttachmentMessage) (*ProcessUserInputResult, error) {
	// Build content blocks
	var content []types.ContentBlock

	// Add text content
	if input != "" {
		content = append(content, types.ContentBlock{Type: "text", Text: input})
	}

	// Add image blocks
	content = append(content, imageBlocks...)

	// Create user message
	userMsg := types.Message{
		Type:      types.MessageTypeUser,
		UUID:      opts.UUID,
		Content:   content,
		IsMeta:    opts.IsMeta,
	}

	// Build result messages
	messages := []types.Message{userMsg}

	// Add attachment messages
	for _, attachMsg := range attachmentMessages {
		msg := types.Message{
			Type:       types.MessageTypeAttachment,
			UUID:       attachMsg.UUID,
			Content:    attachMsg.Content,
			Attachment: attachMsg.Attachment,
		}
		messages = append(messages, msg)
	}

	// Add image metadata if needed
	if len(imagePasteIDs) > 0 {
		var metadataTexts []string
		for _, id := range imagePasteIDs {
			if opts.Context != nil && opts.Context.PastedContents != nil {
				if content, ok := opts.Context.PastedContents[id]; ok {
					metadata := CreateImageMetadataText(detectMediaType(content.Data), len(content.Data))
					metadataTexts = append(metadataTexts, metadata)
				}
			}
		}

		if len(metadataTexts) > 0 {
			metadataMsg := types.Message{
				Type:   types.MessageTypeUser,
				IsMeta: true,
				Content: []types.ContentBlock{
					{Type: "text", Text: strings.Join(metadataTexts, "\n")},
				},
			}
			messages = append(messages, metadataMsg)
		}
	}

	result := &ProcessUserInputResult{
		Messages:    messages,
		ShouldQuery: true,
	}

	return result, nil
}

// ValidateInput validates user input.
func ValidateInput(input string) error {
	if input == "" {
		return fmt.Errorf("input cannot be empty")
	}

	// Check for maximum length
	const maxInputLength = 1000000 // 1MB
	if len(input) > maxInputLength {
		return fmt.Errorf("input exceeds maximum length of %d characters", maxInputLength)
	}

	return nil
}

// TruncateOutput truncates output to a maximum length.
func TruncateOutput(content string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}

	return fmt.Sprintf("%s… [output truncated - exceeded %d characters]",
		content[:maxLength], maxLength)
}

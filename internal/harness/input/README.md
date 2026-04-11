# Input Processing Module

## Overview

The `input` package provides comprehensive user input processing for the Claude Code Go implementation. It handles text input, slash commands, attachments, and image content.

## Features

### 1. User Input Processing
- **Text Input**: Process plain text user messages
- **Content Blocks**: Handle structured content blocks (text, images, etc.)
- **Input Validation**: Validate input length and format
- **Output Truncation**: Truncate long outputs with metadata

### 2. Slash Command Support
- **Command Parsing**: Parse slash commands like `/help`, `/search query`
- **Arguments Extraction**: Extract command arguments
- **Bridge Safety**: Handle commands from bridge/CCR origins
- **Skip Mode**: Option to treat `/` as plain text

### 3. Attachment Processing
- **IDE Selection**: Attach selected code from IDE
- **File Attachments**: Attach files and directories
- **Agent Mentions**: Support @agent-type mentions
- **Attachment References**: Parse @file:path references

### 4. Image Processing
- **Image Content**: Process pasted images
- **Media Type Detection**: Auto-detect PNG, JPEG, GIF, WebP
- **Base64 Encoding**: Encode images for API transmission
- **Image Metadata**: Generate metadata for images
- **Paste ID Tracking**: Track image paste IDs

## Architecture

```
input/
├── process.go          # Main entry point
├── process_test.go     # Core tests
├── attachment.go       # Attachment handling
├── attachment_test.go  # Attachment tests
├── image.go           # Image processing
└── image_test.go      # Image tests
```

## Usage

### Basic Text Input

```go
import (
    "context"
    "claude-codex/internal/harness/input"
)

opts := &input.ProcessUserInputOptions{
    Input: "Hello, how are you?",
    Mode:  "prompt",
    Context: &input.ProcessUserInputContext{
        WorkingDir: "/path/to/project",
        SessionID:  "session-123",
    },
    UUID: "msg-uuid",
}

result, err := input.ProcessUserInput(context.Background(), opts)
if err != nil {
    // Handle error
}

// result.Messages contains the processed messages
// result.ShouldQuery indicates whether to proceed with query
```

### With IDE Selection

```go
opts := &input.ProcessUserInputOptions{
    Input: "Explain this code",
    Mode:  "prompt",
    Context: &input.ProcessUserInputContext{
        WorkingDir: "/path/to/project",
        SessionID:  "session-123",
        IDESelection: &input.IDESelection{
            FilePath:  "main.go",
            StartLine: 10,
            EndLine:   20,
            Content:   "func main() { ... }",
        },
    },
    UUID: "msg-uuid",
}

result, err := input.ProcessUserInput(context.Background(), opts)
```

### With Images

```go
opts := &input.ProcessUserInputOptions{
    Input: "Check this screenshot:\n[Pasted image #1]",
    Mode:  "prompt",
    Context: &input.ProcessUserInputContext{
        WorkingDir: "/path/to/project",
        SessionID:  "session-123",
        PastedContents: map[int]input.PastedContent{
            1: {
                Type: "image",
                Data: imageBytes, // PNG/JPEG/GIF/WebP
            },
        },
    },
    UUID: "msg-uuid",
}

result, err := input.ProcessUserInput(context.Background(), opts)
```

### Slash Commands

```go
opts := &input.ProcessUserInputOptions{
    Input: "/help",
    Mode:  "prompt",
    Context: &input.ProcessUserInputContext{
        WorkingDir: "/path/to/project",
        SessionID:  "session-123",
    },
    UUID: "msg-uuid",
    SkipSlashCommands: false, // Process slash commands
}

result, err := input.ProcessUserInput(context.Background(), opts)
```

## Types

### ProcessUserInputOptions

```go
type ProcessUserInputOptions struct {
    Input             any    // string or []types.ContentBlock
    PreExpansionInput string // Input before expansion
    Mode              string // "prompt", "edit", etc.
    Context           *ProcessUserInputContext
    UUID              string
    QuerySource       string
    SkipSlashCommands bool
    BridgeOrigin      bool
    IsMeta            bool
    SkipAttachments   bool
    IsAlreadyProcessing bool
}
```

### ProcessUserInputResult

```go
type ProcessUserInputResult struct {
    Messages        []types.Message
    ShouldQuery     bool
    AllowedTools    []string
    Model           string
    ResultText      string
    NextInput       string
    SubmitNextInput bool
}
```

### ProcessUserInputContext

```go
type ProcessUserInputContext struct {
    WorkingDir     string
    SessionID      string
    PermissionMode string
    Messages       []types.Message
    IDESelection   *IDESelection
    PastedContents map[int]PastedContent
}
```

## Image Processing

### Supported Formats
- PNG (image/png)
- JPEG (image/jpeg)
- GIF (image/gif)
- WebP (image/webp)

### Media Type Detection

The package automatically detects image formats by examining file signatures:

```go
mediaType := detectMediaType(imageData)
// Returns: "image/png", "image/jpeg", "image/gif", or "image/webp"
```

### Image Content Blocks

Images are converted to content blocks with base64-encoded data:

```go
imageBlocks, imagePasteIDs, err := ProcessImageContent(pastedContents)
```

## Attachment Processing

### Attachment Types

```go
const (
    AttachmentTypeFile         AttachmentType = "file"
    AttachmentTypeDirectory    AttachmentType = "directory"
    AttachmentTypeIDESelection AttachmentType = "ide_selection"
    AttachmentTypeAgentMention AttachmentType = "agent_mention"
    AttachmentTypeTask         AttachmentType = "task"
    AttachmentTypePlan         AttachmentType = "plan"
)
```

### Creating Attachments

```go
// File attachment
fileMsg, err := CreateFileAttachment("/path/to/file.go")

// Agent mention
agentMsg := CreateAgentMentionAttachment("executor")

// IDE selection (automatic via ProcessUserInput)
```

## Validation

### Input Validation

```go
err := ValidateInput(userInput)
// Checks:
// - Non-empty input
// - Maximum length (1MB)
```

### Output Truncation

```go
truncated := TruncateOutput(longContent, maxLength)
// Adds "… [output truncated - exceeded N characters]" if needed
```

## Testing

The package includes comprehensive tests:

```bash
# Run all tests
go test -v

# Run specific test
go test -v -run TestProcessUserInput

# Run with coverage
go test -cover
```

### Test Coverage

- ✅ Basic text input processing
- ✅ Slash command parsing
- ✅ Content block handling
- ✅ Image processing (all formats)
- ✅ Attachment processing (all types)
- ✅ IDE selection
- ✅ Input validation
- ✅ Output truncation
- ✅ Error handling

## Integration with QueryEngine

This module integrates with the QueryEngine to process user input before sending to the API:

```go
// In QueryEngine.Submit()
result, err := input.ProcessUserInput(ctx, &input.ProcessUserInputOptions{
    Input:   userInput,
    Mode:    "prompt",
    Context: inputContext,
    UUID:    uuid,
})

if !result.ShouldQuery {
    return // Don't proceed with query
}

// Add messages to conversation
for _, msg := range result.Messages {
    conversation = append(conversation, msg)
}
```

## TypeScript Source Reference

This implementation is based on:
- `src/utils/processUserInput/processUserInput.ts`
- `src/utils/processUserInput/processTextPrompt.ts`
- `src/utils/attachments.ts`
- `src/utils/slashCommandParsing.ts`
- `src/utils/imageResizer.ts`

## Future Enhancements

- [ ] Slash command execution (currently treated as regular input)
- [ ] Bridge-safe command filtering
- [ ] Image resizing and downsampling
- [ ] File attachment size limits
- [ ] Attachment caching
- [ ] Hook integration (UserPromptSubmit hooks)
- [ ] Ultraplan keyword detection
- [ ] Agent mention parsing

## Performance

- **Input validation**: O(1) for length check
- **Image processing**: O(n) where n = number of images
- **Attachment processing**: O(m) where m = number of attachments
- **Media type detection**: O(1) signature check

## Error Handling

All functions return descriptive errors:

```go
result, err := ProcessUserInput(ctx, opts)
if err != nil {
    // Possible errors:
    // - "options cannot be nil"
    // - "failed to get input string: ..."
    // - "failed to process attachments: ..."
    // - "failed to process image content: ..."
}
```

## License

Part of the Claude Code Go implementation.

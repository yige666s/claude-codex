package input

import (
	"context"
	"testing"

	"claude-codex/internal/public/types"
)

func TestProcessUserInput_RegularInput(t *testing.T) {
	ctx := context.Background()

	opts := &ProcessUserInputOptions{
		Input: "Hello, how are you?",
		Mode:  "prompt",
		Context: &ProcessUserInputContext{
			WorkingDir: "/test",
			SessionID:  "test-session",
		},
		UUID: "test-uuid",
	}

	result, err := ProcessUserInput(ctx, opts)
	if err != nil {
		t.Fatalf("ProcessUserInput failed: %v", err)
	}

	if !result.ShouldQuery {
		t.Error("Expected ShouldQuery to be true")
	}

	if len(result.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result.Messages))
	}

	msg := result.Messages[0]
	if msg.Type != types.MessageTypeUser {
		t.Errorf("Expected message type user, got %s", msg.Type)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(msg.Content))
	}

	if msg.Content[0].Text != "Hello, how are you?" {
		t.Errorf("Expected text 'Hello, how are you?', got '%s'", msg.Content[0].Text)
	}
}

func TestProcessUserInput_SlashCommand(t *testing.T) {
	ctx := context.Background()

	opts := &ProcessUserInputOptions{
		Input: "/help",
		Mode:  "prompt",
		Context: &ProcessUserInputContext{
			WorkingDir: "/test",
			SessionID:  "test-session",
		},
		UUID: "test-uuid",
	}

	result, err := ProcessUserInput(ctx, opts)
	if err != nil {
		t.Fatalf("ProcessUserInput failed: %v", err)
	}

	// For now, slash commands are treated as regular input
	if !result.ShouldQuery {
		t.Error("Expected ShouldQuery to be true")
	}
}

func TestProcessUserInput_SkipSlashCommands(t *testing.T) {
	ctx := context.Background()

	opts := &ProcessUserInputOptions{
		Input:             "/help",
		Mode:              "prompt",
		SkipSlashCommands: true,
		Context: &ProcessUserInputContext{
			WorkingDir: "/test",
			SessionID:  "test-session",
		},
		UUID: "test-uuid",
	}

	result, err := ProcessUserInput(ctx, opts)
	if err != nil {
		t.Fatalf("ProcessUserInput failed: %v", err)
	}

	if !result.ShouldQuery {
		t.Error("Expected ShouldQuery to be true")
	}

	// Should treat as regular input
	if len(result.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result.Messages))
	}
}

func TestProcessUserInput_ContentBlocks(t *testing.T) {
	ctx := context.Background()

	opts := &ProcessUserInputOptions{
		Input: []types.ContentBlock{
			{Type: "text", Text: "Hello"},
			{Type: "text", Text: "World"},
		},
		Mode: "prompt",
		Context: &ProcessUserInputContext{
			WorkingDir: "/test",
			SessionID:  "test-session",
		},
		UUID: "test-uuid",
	}

	result, err := ProcessUserInput(ctx, opts)
	if err != nil {
		t.Fatalf("ProcessUserInput failed: %v", err)
	}

	if !result.ShouldQuery {
		t.Error("Expected ShouldQuery to be true")
	}

	if len(result.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(result.Messages))
	}
}

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid input",
			input:   "Hello, world!",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "very long input",
			input:   string(make([]byte, 2000000)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		maxLength int
		want      string
	}{
		{
			name:      "short content",
			content:   "Hello",
			maxLength: 10,
			want:      "Hello",
		},
		{
			name:      "exact length",
			content:   "Hello",
			maxLength: 5,
			want:      "Hello",
		},
		{
			name:      "needs truncation",
			content:   "Hello, world!",
			maxLength: 5,
			want:      "Hello… [output truncated - exceeded 5 characters]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateOutput(tt.content, tt.maxLength)
			if got != tt.want {
				t.Errorf("TruncateOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantCommand string
		wantArgs    string
	}{
		{
			name:        "simple command",
			input:       "/help",
			wantCommand: "help",
			wantArgs:    "",
		},
		{
			name:        "command with args",
			input:       "/search query text",
			wantCommand: "search",
			wantArgs:    "query text",
		},
		{
			name:        "command with multiple spaces",
			input:       "/commit -m 'test message'",
			wantCommand: "commit",
			wantArgs:    "-m 'test message'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCommand, gotArgs := parseSlashCommand(tt.input)
			if gotCommand != tt.wantCommand {
				t.Errorf("parseSlashCommand() command = %v, want %v", gotCommand, tt.wantCommand)
			}
			if gotArgs != tt.wantArgs {
				t.Errorf("parseSlashCommand() args = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}

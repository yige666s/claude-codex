package agent

import (
	"fmt"
)

// Fork creates a forked agent that inherits parent context
type Fork struct {
	executor *Executor
}

// NewFork creates a new fork manager
func NewFork(executor *Executor) *Fork {
	return &Fork{
		executor: executor,
	}
}

// ForkConfig contains configuration for forking an agent
type ForkConfig struct {
	ParentID       AgentID
	ParentModel    string
	ParentMessages []Message
	Directive      string
	WorkingDir     string
	SystemPrompt   string
}

// FORK_SUBAGENT_TYPE is the synthetic agent type for forks
const FORK_SUBAGENT_TYPE = "fork"

// FORK_BOILERPLATE_TAG marks fork boilerplate in messages
const FORK_BOILERPLATE_TAG = "fork-boilerplate"

// FORK_DIRECTIVE_PREFIX prefixes the directive in fork messages
const FORK_DIRECTIVE_PREFIX = "Your directive: "

// ForkToolResultPlaceholder answers inherited tool_use blocks so the forked
// child receives a syntactically complete transcript without pretending the
// parent's background tool has already produced a final result.
const ForkToolResultPlaceholder = "Forked subagent started in the background. Its final result will be delivered as a task notification when complete."

// ForkAgent is the synthetic agent definition for forks
var ForkAgent = &AgentDefinition{
	AgentType:    FORK_SUBAGENT_TYPE,
	WhenToUse:    "Implicit fork — inherits full conversation context",
	Tools:        []string{"*"},
	MaxTurns:     200,
	Model:        ModelInherit,
	Permission:   PermissionBubble,
	Source:       SourceBuiltIn,
	BaseDir:      "built-in",
	SystemPrompt: "",
}

// IsInForkChild checks if messages contain fork boilerplate
func IsInForkChild(messages []Message) bool {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "text" && containsForkBoilerplate(block.Text) {
				return true
			}
		}
	}
	return false
}

func containsForkBoilerplate(text string) bool {
	return len(text) > 0 && (text[0] == '<' || len(text) > 100) &&
		(containsSubstring(text, "<"+FORK_BOILERPLATE_TAG+">"))
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// BuildForkedMessages creates the message sequence for a fork child
func BuildForkedMessages(directive string, assistantMessage Message) []Message {
	// Clone assistant message
	forkedAssistant := Message{
		ID:        generateMessageID(),
		Role:      assistantMessage.Role,
		Content:   make([]ContentBlock, len(assistantMessage.Content)),
		Timestamp: assistantMessage.Timestamp,
	}
	copy(forkedAssistant.Content, assistantMessage.Content)

	// Collect tool_use blocks
	var toolUseBlocks []ContentBlock
	for _, block := range assistantMessage.Content {
		if block.Type == "tool_use" {
			toolUseBlocks = append(toolUseBlocks, block)
		}
	}

	// If no tool uses, just return user message with directive
	if len(toolUseBlocks) == 0 {
		return []Message{
			{
				ID:   generateMessageID(),
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: buildChildMessage(directive)},
				},
			},
		}
	}

	// Build tool_result blocks with placeholder.
	toolResultBlocks := make([]ContentBlock, len(toolUseBlocks))
	for i, toolUse := range toolUseBlocks {
		toolResultBlocks[i] = ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ToolID,
			Result:    ForkToolResultPlaceholder,
			IsError:   false,
		}
	}

	// Build user message: tool results + directive
	userContent := append(toolResultBlocks, ContentBlock{
		Type: "text",
		Text: buildChildMessage(directive),
	})

	return []Message{
		forkedAssistant,
		{
			ID:      generateMessageID(),
			Role:    "user",
			Content: userContent,
		},
	}
}

// buildChildMessage creates the fork child's directive message
func buildChildMessage(directive string) string {
	return fmt.Sprintf(`<%s>
You are a forked subagent. Your parent delegated a specific task to you.

Core rules:
1. Stay strictly within your directive's scope. Do not expand beyond what was asked.
2. Work autonomously — do not ask the user for input or confirmation.
3. Use your tools directly: Bash, Read, Write, etc.
4. If you modify files, commit your changes before reporting. Include the commit hash in your report.
5. Do NOT emit text between tool calls. Use tools silently, then report once at the end.
6. Stay strictly within your directive's scope. If you discover related systems outside your scope, mention them in one sentence at most — other workers cover those areas.
7. Keep your report under 500 words unless the directive specifies otherwise. Be factual and concise.
8. Your response MUST begin with "Scope:". No preamble, no thinking-out-loud.
9. REPORT structured facts, then stop

Output format (plain text labels, not markdown headers):
  Scope: <echo back your assigned scope in one sentence>
  Result: <the answer or key findings, limited to the scope above>
  Key files: <relevant file paths — include for research tasks>
  Files changed: <list with commit hash — include only if you modified files>
  Issues: <list — include only if there are issues to flag>
</%s>

%s%s`, FORK_BOILERPLATE_TAG, FORK_BOILERPLATE_TAG, FORK_DIRECTIVE_PREFIX, directive)
}

// BuildWorktreeNotice creates a notice for worktree-isolated forks
func BuildWorktreeNotice(parentCwd, worktreeCwd string) string {
	return fmt.Sprintf(`You've inherited the conversation context above from a parent agent working in %s. You are operating in an isolated git worktree at %s — same repository, same relative file structure, separate working copy. Paths in the inherited context refer to the parent's working directory; translate them to your worktree root. Re-read files before editing if the parent may have modified them since they appear in the context. Your changes stay in this worktree and will not affect the parent's files.`, parentCwd, worktreeCwd)
}

package memdir

import (
	"fmt"
	"os"
	"strings"
)

const (
	EntrypointName      = "MEMORY.md"
	MaxEntrypointLines  = 200
	MaxEntrypointBytes  = 25000
	AutoMemDisplayName  = "auto memory"
	DirExistsGuidance   = "This directory already exists — write to it directly with the Write tool (do not run mkdir or check for its existence)."
	DirsExistGuidance   = "Both directories already exist — write to them directly with the Write tool (do not run mkdir or check for their existence)."
)

// EntrypointTruncation represents truncation info for MEMORY.md
type EntrypointTruncation struct {
	Content          string
	LineCount        int
	ByteCount        int
	WasLineTruncated bool
	WasByteTruncated bool
}

// TruncateEntrypointContent truncates MEMORY.md content to line AND byte caps
func TruncateEntrypointContent(raw string) EntrypointTruncation {
	trimmed := strings.TrimSpace(raw)
	contentLines := strings.Split(trimmed, "\n")
	lineCount := len(contentLines)
	byteCount := len(trimmed)

	wasLineTruncated := lineCount > MaxEntrypointLines
	wasByteTruncated := byteCount > MaxEntrypointBytes

	if !wasLineTruncated && !wasByteTruncated {
		return EntrypointTruncation{
			Content:          trimmed,
			LineCount:        lineCount,
			ByteCount:        byteCount,
			WasLineTruncated: false,
			WasByteTruncated: false,
		}
	}

	// Line truncation first
	truncated := trimmed
	if wasLineTruncated {
		truncated = strings.Join(contentLines[:MaxEntrypointLines], "\n")
	}

	// Byte truncation at last newline
	if len(truncated) > MaxEntrypointBytes {
		cutAt := strings.LastIndex(truncated[:MaxEntrypointBytes], "\n")
		if cutAt > 0 {
			truncated = truncated[:cutAt]
		} else {
			truncated = truncated[:MaxEntrypointBytes]
		}
	}

	// Build warning message
	var reason string
	if wasByteTruncated && !wasLineTruncated {
		reason = fmt.Sprintf("%d bytes (limit: %d) — index entries are too long", byteCount, MaxEntrypointBytes)
	} else if wasLineTruncated && !wasByteTruncated {
		reason = fmt.Sprintf("%d lines (limit: %d)", lineCount, MaxEntrypointLines)
	} else {
		reason = fmt.Sprintf("%d lines and %d bytes", lineCount, byteCount)
	}

	warning := fmt.Sprintf("\n\n> WARNING: %s is %s. Only part of it was loaded. Keep index entries to one line under ~200 chars; move detail into topic files.", EntrypointName, reason)

	return EntrypointTruncation{
		Content:          truncated + warning,
		LineCount:        lineCount,
		ByteCount:        byteCount,
		WasLineTruncated: wasLineTruncated,
		WasByteTruncated: wasByteTruncated,
	}
}

// EnsureMemoryDirExists ensures a memory directory exists
func EnsureMemoryDirExists(memoryDir string) error {
	return os.MkdirAll(memoryDir, 0755)
}

// BuildMemoryPrompt builds the memory system prompt
func BuildMemoryPrompt(projectRoot string, extraGuidelines []string, skipIndex bool) (string, error) {
	if !IsAutoMemoryEnabled() {
		return "", nil
	}

	autoDir := GetAutoMemPath(projectRoot)

	// Ensure directory exists
	if err := EnsureMemoryDirExists(autoDir); err != nil {
		return "", err
	}

	// Check if team memory is enabled
	teamEnabled := IsTeamMemoryEnabled()
	if teamEnabled {
		teamDir := GetTeamMemPath(projectRoot)
		if err := EnsureMemoryDirExists(teamDir); err != nil {
			return "", err
		}
		return buildCombinedMemoryPrompt(autoDir, teamDir, extraGuidelines, skipIndex), nil
	}

	return buildMemoryPrompt(autoDir, extraGuidelines, skipIndex), nil
}

// buildMemoryPrompt builds prompt for auto memory only
func buildMemoryPrompt(autoDir string, extraGuidelines []string, skipIndex bool) string {
	lines := []string{
		"# auto memory",
		"",
		fmt.Sprintf("You have a persistent, file-based memory system at `%s`. %s", autoDir, DirExistsGuidance),
		"",
		"You should build up this memory system over time so that future conversations can have a complete picture of who the user is, how they'd like to collaborate with you, what behaviors to avoid or repeat, and the context behind the work the user gives you.",
		"",
		"If the user explicitly asks you to remember something, save it immediately as whichever type fits best. If they ask you to forget something, find and remove the relevant entry.",
		"",
	}

	// Add type definitions
	lines = append(lines, getTypesSection()...)
	lines = append(lines, "")

	// Add what not to save
	lines = append(lines, getWhatNotToSaveSection()...)
	lines = append(lines, "")

	// Add how to save
	lines = append(lines, getHowToSaveSection(skipIndex)...)
	lines = append(lines, "")

	// Add when to access
	lines = append(lines, getWhenToAccessSection()...)
	lines = append(lines, "")

	// Add trusting recall section
	lines = append(lines, getTrustingRecallSection()...)
	lines = append(lines, "")

	// Add extra guidelines
	if len(extraGuidelines) > 0 {
		lines = append(lines, extraGuidelines...)
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// buildCombinedMemoryPrompt builds prompt for both auto and team memory
func buildCombinedMemoryPrompt(autoDir, teamDir string, extraGuidelines []string, skipIndex bool) string {
	lines := []string{
		"# Memory",
		"",
		fmt.Sprintf("You have a persistent, file-based memory system with two directories: a private directory at `%s` and a shared team directory at `%s`. %s", autoDir, teamDir, DirsExistGuidance),
		"",
		"You should build up this memory system over time so that future conversations can have a complete picture of who the user is, how they'd like to collaborate with you, what behaviors to avoid or repeat, and the context behind the work the user gives you.",
		"",
		"If the user explicitly asks you to remember something, save it immediately as whichever type fits best. If they ask you to forget something, find and remove the relevant entry.",
		"",
		"## Memory scope",
		"",
		"There are two scope levels:",
		"",
		fmt.Sprintf("- private: memories that are private between you and the current user. They persist across conversations with only this specific user and are stored at the root `%s`.", autoDir),
		fmt.Sprintf("- team: memories that are shared with and contributed by all of the users who work within this project directory. Team memories are synced at the beginning of every session and they are stored at `%s`.", teamDir),
		"",
	}

	// Add combined types section
	lines = append(lines, getCombinedTypesSection()...)
	lines = append(lines, "")

	// Add what not to save
	lines = append(lines, getWhatNotToSaveSection()...)
	lines = append(lines, "- You MUST avoid saving sensitive data within shared team memories. For example, never save API keys or user credentials.")
	lines = append(lines, "")

	// Add how to save
	lines = append(lines, getCombinedHowToSaveSection(skipIndex)...)
	lines = append(lines, "")

	// Add when to access
	lines = append(lines, getWhenToAccessSection()...)
	lines = append(lines, "")

	// Add trusting recall section
	lines = append(lines, getTrustingRecallSection()...)
	lines = append(lines, "")

	// Add extra guidelines
	if len(extraGuidelines) > 0 {
		lines = append(lines, extraGuidelines...)
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// Helper functions for prompt sections

func getTypesSection() []string {
	return []string{
		"## Types of memory",
		"",
		"There are several discrete types of memory that you can store in your memory system:",
		"",
		"<types>",
		"<type>",
		"    <name>user</name>",
		"    <description>Contain information about the user's role, goals, responsibilities, and knowledge.</description>",
		"    <when_to_save>When you learn any details about the user's role, preferences, responsibilities, or knowledge</when_to_save>",
		"</type>",
		"<type>",
		"    <name>feedback</name>",
		"    <description>Guidance the user has given you about how to approach work — both what to avoid and what to keep doing.</description>",
		"    <when_to_save>Any time the user corrects your approach or confirms a non-obvious approach worked</when_to_save>",
		"</type>",
		"<type>",
		"    <name>project</name>",
		"    <description>Information about ongoing work, goals, initiatives, bugs, or incidents within the project.</description>",
		"    <when_to_save>When you learn who is doing what, why, or by when</when_to_save>",
		"</type>",
		"<type>",
		"    <name>reference</name>",
		"    <description>Pointers to where information can be found in external systems.</description>",
		"    <when_to_save>When you learn about resources in external systems and their purpose</when_to_save>",
		"</type>",
		"</types>",
	}
}

func getCombinedTypesSection() []string {
	// Similar to getTypesSection but with scope guidance
	return getTypesSection() // Simplified for now
}

func getWhatNotToSaveSection() []string {
	return []string{
		"## What NOT to save in memory",
		"",
		"- Code patterns, conventions, architecture, file paths, or project structure — these can be derived by reading the current project state.",
		"- Git history, recent changes, or who-changed-what — `git log` / `git blame` are authoritative.",
		"- Debugging solutions or fix recipes — the fix is in the code; the commit message has the context.",
		"- Anything already documented in CLAUDE.md files.",
		"- Ephemeral task details: in-progress work, temporary state, current conversation context.",
	}
}

func getHowToSaveSection(skipIndex bool) []string {
	if skipIndex {
		return []string{
			"## How to save memories",
			"",
			"Write each memory to its own file using this frontmatter format:",
			"",
			"```markdown",
			"---",
			"name: {{memory name}}",
			"description: {{one-line description}}",
			"type: {{user, feedback, project, reference}}",
			"---",
			"",
			"{{memory content}}",
			"```",
		}
	}

	return []string{
		"## How to save memories",
		"",
		"Saving a memory is a two-step process:",
		"",
		"**Step 1** — write the memory to its own file using this frontmatter format:",
		"",
		"```markdown",
		"---",
		"name: {{memory name}}",
		"description: {{one-line description}}",
		"type: {{user, feedback, project, reference}}",
		"---",
		"",
		"{{memory content}}",
		"```",
		"",
		fmt.Sprintf("**Step 2** — add a pointer to that file in `%s`. Each entry should be one line: `- [Title](file.md) — one-line hook`.", EntrypointName),
	}
}

func getCombinedHowToSaveSection(skipIndex bool) []string {
	return getHowToSaveSection(skipIndex) // Simplified
}

func getWhenToAccessSection() []string {
	return []string{
		"## When to access memories",
		"- When memories seem relevant, or the user references prior-conversation work.",
		"- You MUST access memory when the user explicitly asks you to check, recall, or remember.",
		"- If the user says to *ignore* or *not use* memory: proceed as if MEMORY.md were empty.",
	}
}

func getTrustingRecallSection() []string {
	return []string{
		"## Before recommending from memory",
		"",
		"A memory that names a specific function, file, or flag is a claim that it existed *when the memory was written*. It may have been renamed, removed, or never merged. Before recommending it:",
		"",
		"- If the memory names a file path: check the file exists.",
		"- If the memory names a function or flag: grep for it.",
		"- If the user is about to act on your recommendation (not just asking about history), verify first.",
		"",
		`"The memory says X exists" is not the same as "X exists now."`,
	}
}

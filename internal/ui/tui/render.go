package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/ding/claude-code/claude-go/internal/harness/state"
)

func renderTranscript(session *state.Session, renderer *glamour.TermRenderer, styles themeStyles, width int, pendingText string) string {
	if session == nil {
		return "No messages yet."
	}

	blockWidth := width
	if blockWidth < 20 {
		blockWidth = 20
	}

	parts := make([]string, 0, len(session.Messages)+1)
	lastRole := ""

	for _, message := range session.Messages {
		if message.Hidden {
			continue
		}
		switch message.Role {
		case "assistant":
			content := message.Content
			if renderer != nil && strings.TrimSpace(content) != "" {
				rendered, err := renderer.Render(content)
				if err == nil {
					content = strings.TrimSpace(rendered)
				}
			}
			block := styles.assistantBlock.Width(blockWidth).Render(
				styles.assistant.Bold(true).Render("Assistant") + "\n" + content,
			)
			parts = append(parts, block)
			lastRole = "assistant"
		case "user":
			block := styles.userBlock.Width(blockWidth).Render(
				styles.user.Render("User") + "\n" + message.Content,
			)
			parts = append(parts, block)
			lastRole = "user"
		case "tool":
			// Style tool results with subtle formatting
			toolBlock := styles.subtle.Render(
				fmt.Sprintf("🔧 %s", message.ToolName),
			)
			parts = append(parts, toolBlock)
			lastRole = "tool"
		default:
			parts = append(parts, message.Content)
			lastRole = message.Role
		}
	}

	// Show in-progress streaming assistant block with a blinking cursor indicator
	if pendingText != "" {
		block := styles.assistantBlock.Width(blockWidth).Render(
			styles.assistant.Bold(true).Render("Assistant") + "\n" + pendingText + "▌",
		)
		parts = append(parts, block)
	} else if lastRole == "assistant" && len(parts) > 0 {
		// Add completion indicator after assistant's final message
		parts = append(parts, styles.subtle.Render("✓ Done"))
	}

	if len(parts) == 0 {
		return "No messages yet."
	}
	return strings.Join(parts, "\n")
}

func itoa(value int) string {
	return fmt.Sprintf("%d", value)
}

func formatCost(value float64) string {
	return fmt.Sprintf("%.6f", value)
}

func renderBlock(style lipgloss.Style, width int, content string) string {
	if width > 0 {
		return style.Width(width).Render(content)
	}
	return style.Render(content)
}

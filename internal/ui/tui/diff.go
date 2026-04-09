package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/state"
)

func renderDiffSummary(message *state.Message) string {
	if message == nil || message.Role != "tool" {
		return "Diff panel: no file mutation selected."
	}

	switch message.ToolName {
	case "file_write":
		var payload struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(message.ToolInput, &payload); err == nil {
			return fmt.Sprintf("Write\n- Path: %s\n- Bytes: %d", payload.Path, len(payload.Content))
		}
	case "file_edit":
		var payload struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal(message.ToolInput, &payload); err == nil {
			return fmt.Sprintf(
				"Edit\n- Path: %s\n- Old: %s\n- New: %s",
				payload.Path,
				snippet(payload.OldString),
				snippet(payload.NewString),
			)
		}
	}

	if strings.TrimSpace(message.ToolOutput) != "" {
		return "Latest tool\n" + message.ToolOutput
	}
	return "Diff panel: no structured diff available."
}

func snippet(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 48 {
		return value
	}
	return value[:48] + "..."
}

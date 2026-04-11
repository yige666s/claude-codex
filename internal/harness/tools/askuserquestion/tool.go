package askuserquestion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const description = `Asks the user multiple choice questions to gather information, clarify ambiguity, understand preferences, make decisions or offer them choices.

Use this tool when you need to ask the user questions during execution. This allows you to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices as you work
4. Offer choices to the user about what direction to take.

Usage notes:
- Users will always be able to select "Other" to provide custom text input
- Use multiSelect: true to allow multiple answers to be selected for a question
- If you recommend a specific option, make that the first option in the list and add "(Recommended)" at the end of the label

Plan mode note: In plan mode, use this tool to clarify requirements or choose between approaches BEFORE finalizing your plan. Do NOT use this tool to ask "Is my plan ready?" or "Should I proceed?" - use ExitPlanMode for plan approval.`

// Tool implements the AskUserQuestion tool.
type Tool struct{}

type option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

type question struct {
	Question    string   `json:"question"`
	Header      string   `json:"header"`
	Options     []option `json:"options,omitempty"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

type inputPayload struct {
	Questions   []question             `json:"questions"`
	Answers     map[string]string      `json:"answers,omitempty"`
	Annotations map[string]interface{} `json:"annotations,omitempty"`
}

func NewTool() *Tool {
	return &Tool{}
}

func (t *Tool) Name() string {
	return "AskUserQuestion"
}

func (t *Tool) Description() string {
	return description
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "questions": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "question": {"type": "string"},
          "header": {"type": "string", "maxLength": 12},
          "options": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "label": {"type": "string"},
                "description": {"type": "string"},
                "preview": {"type": "string"}
              },
              "required": ["label"]
            }
          },
          "multiSelect": {"type": "boolean", "default": false}
        },
        "required": ["question", "header"]
      }
    },
    "answers": {
      "type": "object",
      "additionalProperties": {"type": "string"}
    },
    "annotations": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "properties": {
          "notes": {"type": "string"},
          "preview": {"type": "string"}
        }
      }
    }
  },
  "required": ["questions"]
}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *Tool) IsConcurrencySafe() bool {
	return false
}

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var payload inputPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return toolkit.Result{}, err
	}

	var sb strings.Builder
	for i, q := range payload.Questions {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "[%s] %s\n", q.Header, q.Question)
		for j, opt := range q.Options {
			if opt.Description != "" {
				fmt.Fprintf(&sb, "  %d. %s — %s\n", j+1, opt.Label, opt.Description)
			} else {
				fmt.Fprintf(&sb, "  %d. %s\n", j+1, opt.Label)
			}
		}
		if q.MultiSelect {
			sb.WriteString("  (Multiple selections allowed)\n")
		}
	}

	return toolkit.Result{Output: sb.String()}, nil
}

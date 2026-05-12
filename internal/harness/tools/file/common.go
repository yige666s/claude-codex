package file

import (
	"encoding/json"
	"strings"
)

const (
	ReadToolName  = "Read"
	WriteToolName = "Write"
	EditToolName  = "Edit"
)

type patchHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

type writeOutput struct {
	Type            string      `json:"type"`
	FilePath        string      `json:"filePath"`
	Content         string      `json:"content"`
	StructuredPatch []patchHunk `json:"structuredPatch"`
	OriginalFile    *string     `json:"originalFile"`
}

type editOutput struct {
	FilePath        string      `json:"filePath"`
	OldString       string      `json:"oldString"`
	NewString       string      `json:"newString"`
	OriginalFile    string      `json:"originalFile"`
	StructuredPatch []patchHunk `json:"structuredPatch"`
	UserModified    bool        `json:"userModified"`
	ReplaceAll      bool        `json:"replaceAll"`
}

func encodeOutput(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func structuredPatch(oldContent, newContent string) []patchHunk {
	if oldContent == newContent {
		return []patchHunk{}
	}

	lines := make([]string, 0, lineCount(oldContent)+lineCount(newContent))
	for _, line := range splitPatchLines(oldContent) {
		lines = append(lines, "-"+line)
	}
	for _, line := range splitPatchLines(newContent) {
		lines = append(lines, "+"+line)
	}

	return []patchHunk{{
		OldStart: 1,
		OldLines: lineCount(oldContent),
		NewStart: 1,
		NewLines: lineCount(newContent),
		Lines:    lines,
	}}
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return len(splitPatchLines(content))
}

func splitPatchLines(content string) []string {
	if content == "" {
		return nil
	}
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

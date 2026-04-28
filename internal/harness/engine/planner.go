package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type ToolCall = plannerapi.ToolCall
type Plan = plannerapi.Plan
type Planner = plannerapi.Planner

type SimplePlanner struct{}

var (
	pathPattern     = regexp.MustCompile(`([A-Za-z0-9_./-]+\.[A-Za-z0-9_]+)`)
	urlPattern      = regexp.MustCompile(`https?://[^\s"'` + "`" + `]+`)
	backtickPattern = regexp.MustCompile("`([^`]+)`")
	quotePattern    = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
)

func NewSimplePlanner() *SimplePlanner {
	return &SimplePlanner{}
}

func (p *SimplePlanner) Next(_ context.Context, session *state.Session, _ []toolkit.Descriptor) (Plan, error) {
	last := session.LastMessage()
	if last != nil && last.Role == "tool" {
		return Plan{
			AssistantText: summarizeTool(*last),
			StopReason:    "end_turn",
		}, nil
	}

	prompt := session.LastUserMessage()
	if prompt == "" {
		return Plan{
			AssistantText: "No prompt was provided.",
			StopReason:    "end_turn",
		}, nil
	}

	if call, ok, err := planCreateFile(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planReadFile(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planEditFile(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planGlob(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planGrep(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planWebSearch(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planWebFetch(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planNotebook(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planAgent(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	if call, ok, err := planBash(prompt); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{ToolCalls: []ToolCall{call}, StopReason: "tool_use"}, nil
	}

	return Plan{
		AssistantText: "The simple planner currently supports file create/read/edit, glob, grep, web search/fetch, notebook edits, agent delegation, and running one shell command.",
		StopReason:    "end_turn",
	}, nil
}

func summarizeTool(message state.Message) string {
	if strings.TrimSpace(message.ToolOutput) != "" {
		return message.ToolOutput
	}
	if message.ToolName != "" {
		return fmt.Sprintf("completed %s", message.ToolName)
	}
	return "completed tool execution"
}

func planCreateFile(prompt string) (ToolCall, bool, error) {
	if !containsAny(strings.ToLower(prompt), "create", "write", "new", "创建", "新建", "生成") {
		return ToolCall{}, false, nil
	}

	path := extractPath(prompt)
	if path == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{
		"path":    path,
		"content": defaultFileContent(path),
	})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{
		ID:    "call-file-write",
		Name:  "file_write",
		Input: input,
	}, true, nil
}

func planReadFile(prompt string) (ToolCall, bool, error) {
	if !containsAny(strings.ToLower(prompt), "read", "show", "查看", "读取") {
		return ToolCall{}, false, nil
	}

	path := extractPath(prompt)
	if path == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{
		ID:    "call-file-read",
		Name:  "file_read",
		Input: input,
	}, true, nil
}

func planEditFile(prompt string) (ToolCall, bool, error) {
	if !containsAny(strings.ToLower(prompt), "replace", "替换") {
		return ToolCall{}, false, nil
	}

	path := extractPath(prompt)
	if path == "" {
		return ToolCall{}, false, nil
	}

	parts := extractQuotedStrings(prompt)
	if len(parts) < 2 {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{
		"path":       path,
		"old_string": parts[0],
		"new_string": parts[1],
	})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{
		ID:    "call-file-edit",
		Name:  "file_edit",
		Input: input,
	}, true, nil
}

func planBash(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if !containsAny(lower, "run", "execute", "执行", "运行") {
		return ToolCall{}, false, nil
	}

	command := extractBacktick(prompt)
	if command == "" {
		switch {
		case strings.HasPrefix(lower, "run "):
			command = strings.TrimSpace(prompt[4:])
		case strings.HasPrefix(prompt, "运行 "):
			command = strings.TrimSpace(strings.TrimPrefix(prompt, "运行 "))
		}
	}

	if command == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{"command": command})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{
		ID:    "call-bash",
		Name:  "bash",
		Input: input,
	}, true, nil
}

func planGlob(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, "list files", "files matching", "glob", "列出文件", "匹配文件") {
		return ToolCall{}, false, nil
	}

	pattern := extractQuotedOrBacktick(prompt)
	if pattern == "" {
		pattern = extractPath(prompt)
	}
	if pattern == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{
		"pattern": pattern,
	})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{
		ID:    "call-glob",
		Name:  "glob",
		Input: input,
	}, true, nil
}

func planGrep(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, "grep", "search", "find", "查找", "搜索") || containsAny(lower, "web search", "search web", "online search", "联网搜索", "网页搜索") {
		return ToolCall{}, false, nil
	}

	pattern := extractQuotedOrBacktick(prompt)
	if pattern == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{
		"pattern": pattern,
	})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{
		ID:    "call-grep",
		Name:  "grep",
		Input: input,
	}, true, nil
}

func planWebSearch(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, "web search", "search web", "online search", "联网搜索", "网页搜索") {
		return ToolCall{}, false, nil
	}

	query := extractQuotedOrBacktick(prompt)
	if query == "" {
		query = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(lower, "web search"), "search web"))
	}
	if query == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		return ToolCall{}, false, err
	}

	return ToolCall{ID: "call-web-search", Name: "web_search", Input: input}, true, nil
}

func planWebFetch(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, "fetch", "open url", "visit", "打开网址", "抓取网页") {
		return ToolCall{}, false, nil
	}

	url := extractURL(prompt)
	if url == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{"url": url})
	if err != nil {
		return ToolCall{}, false, err
	}
	return ToolCall{ID: "call-web-fetch", Name: "web_fetch", Input: input}, true, nil
}

func planNotebook(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, ".ipynb", "notebook") {
		return ToolCall{}, false, nil
	}

	path := extractPath(prompt)
	if path == "" {
		return ToolCall{}, false, nil
	}
	content := extractQuotedOrBacktick(prompt)
	if content == "" {
		return ToolCall{}, false, nil
	}

	operation := "append"
	if containsAny(lower, "replace", "替换") {
		operation = "replace"
	}

	input := map[string]any{
		"path":      path,
		"operation": operation,
		"source":    content,
	}
	if operation == "replace" {
		input["index"] = 0
	}

	raw, err := json.Marshal(input)
	if err != nil {
		return ToolCall{}, false, err
	}
	return ToolCall{ID: "call-notebook-edit", Name: "notebook_edit", Input: raw}, true, nil
}

func planAgent(prompt string) (ToolCall, bool, error) {
	lower := strings.ToLower(prompt)
	if !containsAny(lower, "delegate", "subagent", "agent run", "子代理") {
		return ToolCall{}, false, nil
	}

	childPrompt := extractQuotedOrBacktick(prompt)
	if childPrompt == "" {
		return ToolCall{}, false, nil
	}

	input, err := json.Marshal(map[string]any{"prompt": childPrompt})
	if err != nil {
		return ToolCall{}, false, err
	}
	return ToolCall{ID: "call-agent", Name: "agent", Input: input}, true, nil
}

func extractPath(prompt string) string {
	match := pathPattern.FindStringSubmatch(prompt)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractBacktick(prompt string) string {
	match := backtickPattern.FindStringSubmatch(prompt)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractQuotedStrings(prompt string) []string {
	matches := quotePattern.FindAllStringSubmatch(prompt, -1)
	values := make([]string, 0, len(matches))
	for _, match := range matches {
		for _, candidate := range match[1:] {
			if candidate != "" {
				values = append(values, candidate)
				break
			}
		}
	}
	return values
}

func extractQuotedOrBacktick(prompt string) string {
	if value := extractBacktick(prompt); value != "" {
		return value
	}
	values := extractQuotedStrings(prompt)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func extractURL(prompt string) string {
	return urlPattern.FindString(prompt)
}

func defaultFileContent(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	case ".md":
		return "# New Document\n"
	case ".json":
		return "{\n  \"status\": \"ok\"\n}\n"
	case ".txt":
		return "hello\n"
	default:
		return ""
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

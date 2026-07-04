package agentruntime

import (
	"encoding/json"
	"fmt"
	"strings"
)

func toolCallStartEvent(sessionID, toolName, callID string, input any, metadata map[string]any) Event {
	return Event{
		Type:      "tool_call_start",
		ID:        strings.TrimSpace(callID),
		SessionID: strings.TrimSpace(sessionID),
		Role:      "tool",
		Tool:      strings.TrimSpace(toolName),
		Input:     input,
		Summary:   toolCallInputSummary(input),
		Data:      toolCallEventData(toolName, callID, input, "", "running", metadata),
	}
}

func toolCallResultEvent(sessionID, toolName, callID string, input any, output string, err error, metadata map[string]any) Event {
	eventType := "tool_call_result"
	status := "succeeded"
	if err != nil {
		eventType = "tool_call_error"
		status = "failed"
	}
	event := Event{
		Type:      eventType,
		ID:        strings.TrimSpace(callID),
		SessionID: strings.TrimSpace(sessionID),
		Role:      "tool",
		Tool:      strings.TrimSpace(toolName),
		Input:     input,
		Summary:   toolCallOutputSummary(output, err),
		Data:      toolCallEventData(toolName, callID, input, output, status, metadata),
	}
	if err != nil {
		event.Error = err.Error()
	}
	return event
}

func toolCallEventData(toolName, callID string, input any, output, status string, metadata map[string]any) json.RawMessage {
	payload := map[string]any{
		"tool":    strings.TrimSpace(toolName),
		"call_id": strings.TrimSpace(callID),
		"status":  strings.TrimSpace(status),
	}
	if input != nil {
		payload["input"] = input
	}
	if strings.TrimSpace(output) != "" {
		payload["output"] = output
	}
	for key, value := range metadata {
		if value != nil {
			payload[key] = value
		}
	}
	return liveJSON(payload)
}

func toolCallInputSummary(input any) string {
	if input == nil {
		return "Tool call started"
	}
	if raw, ok := input.(json.RawMessage); ok {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err == nil {
			if query := firstNonEmptyString(toolCallMapString(obj, "query"), toolCallMapString(obj, "q"), toolCallMapString(obj, "url")); query != "" {
				return truncateString(query, 160)
			}
		}
	}
	return "Tool call started"
}

func toolCallOutputSummary(output string, err error) string {
	if err != nil {
		return truncateString(err.Error(), 180)
	}
	output = summarizeToolOutput(output)
	if output == "" {
		return "Tool call completed"
	}
	return truncateString(oneLine(output), 180)
}

func summarizeToolOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	paragraphs := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n\n")
	for index := len(paragraphs) - 1; index >= 0; index-- {
		paragraph := strings.TrimSpace(paragraphs[index])
		if paragraph != "" {
			return paragraph
		}
	}
	return output
}

func toolCallMapString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

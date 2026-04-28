package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ClassifierRequest is the compact permission context sent to an auto-mode classifier.
type ClassifierRequest struct {
	ToolName string
	Level    Level
	Summary  string
	Metadata map[string]string
	Mode     Mode
}

// ClassifierResult is the classifier's permission decision.
type ClassifierResult struct {
	Behavior             Behavior
	Reason               string
	Classifier           string
	Confidence           string
	ClassifierApprovable bool
}

// Classifier decides auto-mode permission requests.
type Classifier interface {
	ClassifyPermission(ctx context.Context, request ClassifierRequest) (ClassifierResult, error)
}

// ClassifierFunc adapts a function into a Classifier.
type ClassifierFunc func(context.Context, ClassifierRequest) (ClassifierResult, error)

func (f ClassifierFunc) ClassifyPermission(ctx context.Context, request ClassifierRequest) (ClassifierResult, error) {
	return f(ctx, request)
}

// TextClassifierClient is the narrow LLM surface needed by the permission classifier.
type TextClassifierClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// TextClassifierFunc adapts a function into a TextClassifierClient.
type TextClassifierFunc func(context.Context, string) (string, error)

func (f TextClassifierFunc) Complete(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

// TextClassifier turns a text-completion LLM client into a permission classifier.
type TextClassifier struct {
	client TextClassifierClient
}

func NewTextClassifier(client TextClassifierClient) *TextClassifier {
	return &TextClassifier{client: client}
}

func (c *TextClassifier) ClassifyPermission(ctx context.Context, request ClassifierRequest) (ClassifierResult, error) {
	if c == nil || c.client == nil {
		return ClassifierResult{}, fmt.Errorf("permission classifier client is not configured")
	}
	response, err := c.client.Complete(ctx, BuildClassifierPrompt(request))
	if err != nil {
		return ClassifierResult{}, err
	}
	return ParseClassifierResponse(response)
}

// BuildClassifierPrompt creates a stable prompt for a text LLM classifier.
func BuildClassifierPrompt(request ClassifierRequest) string {
	var b strings.Builder
	b.WriteString("Decide whether this Claude Code tool request is safe to run automatically.\n")
	b.WriteString("Return strict JSON only: {\"behavior\":\"allow|deny|ask\",\"reason\":\"...\",\"confidence\":\"low|medium|high\"}.\n\n")
	b.WriteString("Tool: ")
	b.WriteString(request.ToolName)
	b.WriteString("\nLevel: ")
	b.WriteString(string(request.Level))
	if request.Summary != "" {
		b.WriteString("\nSummary: ")
		b.WriteString(request.Summary)
	}
	if len(request.Metadata) > 0 {
		keys := make([]string, 0, len(request.Metadata))
		for key := range request.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		b.WriteString("\nMetadata:")
		for _, key := range keys {
			b.WriteString("\n- ")
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(request.Metadata[key])
		}
	}
	return b.String()
}

// ParseClassifierResponse parses the strict JSON response from the LLM classifier.
func ParseClassifierResponse(response string) (ClassifierResult, error) {
	var payload struct {
		Behavior   string `json:"behavior"`
		Reason     string `json:"reason"`
		Confidence string `json:"confidence"`
		Classifier string `json:"classifier"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &payload); err != nil {
		return ClassifierResult{}, fmt.Errorf("invalid classifier response: %w", err)
	}

	behavior := Behavior(strings.ToLower(strings.TrimSpace(payload.Behavior)))
	switch behavior {
	case BehaviorAllow, BehaviorDeny, BehaviorAsk:
	default:
		return ClassifierResult{}, fmt.Errorf("invalid classifier behavior %q", payload.Behavior)
	}

	classifier := strings.TrimSpace(payload.Classifier)
	if classifier == "" {
		classifier = "auto-mode"
	}
	return ClassifierResult{
		Behavior:   behavior,
		Reason:     strings.TrimSpace(payload.Reason),
		Classifier: classifier,
		Confidence: strings.TrimSpace(payload.Confidence),
	}, nil
}

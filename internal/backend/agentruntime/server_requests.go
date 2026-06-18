package agentruntime

import (
	"fmt"
	"strings"
)

type adminOpsQuotaResetRequest struct {
	UserID string `json:"user_id" validate:"notblank"`
	Reason string `json:"reason"`
}

type adminOpsQuotaRefundRequest struct {
	UserID        string  `json:"user_id" validate:"notblank"`
	RequestRefund int     `json:"request_refund" validate:"gte=0"`
	TokenRefund   int     `json:"token_refund" validate:"gte=0"`
	CostRefundUSD float64 `json:"cost_refund_usd" validate:"gte=0"`
	Reason        string  `json:"reason"`
}

func (req adminOpsQuotaRefundRequest) ValidateRequest() error {
	if req.RequestRefund == 0 && req.TokenRefund == 0 && req.CostRefundUSD == 0 {
		return fmt.Errorf("at least one refund value is required")
	}
	return nil
}

type createJobRequest struct {
	SessionID      string              `json:"session_id" validate:"notblank"`
	LoopGoalID     string              `json:"loop_goal_id,omitempty"`
	Content        string              `json:"content"`
	Type           string              `json:"type"`
	AttachmentIDs  []string            `json:"attachment_ids"`
	AttachmentURLs []ChatAttachmentURL `json:"attachment_urls"`
}

func (req createJobRequest) ValidateRequest() error {
	return validatePromptPayload(req.Content, req.AttachmentIDs, req.AttachmentURLs)
}

type chatMessageRequest struct {
	Content        string              `json:"content"`
	AttachmentIDs  []string            `json:"attachment_ids"`
	AttachmentURLs []ChatAttachmentURL `json:"attachment_urls"`
	ThinkingMode   bool                `json:"thinking_mode,omitempty"`
	AgentMode      string              `json:"agent_mode,omitempty"`
}

func (req chatMessageRequest) ValidateRequest() error {
	return validatePromptPayload(req.Content, req.AttachmentIDs, req.AttachmentURLs)
}

type loopTriggerHTTPRequest struct {
	SessionID      string              `json:"session_id" validate:"notblank"`
	Objective      string              `json:"objective"`
	TemplateID     string              `json:"template_id,omitempty"`
	TaskType       string              `json:"task_type,omitempty"`
	Deliverable    string              `json:"deliverable,omitempty"`
	Rubric         LoopRubric          `json:"rubric,omitempty"`
	Budget         LoopBudget          `json:"budget,omitempty"`
	StopPolicy     LoopStopPolicy      `json:"stop_policy,omitempty"`
	TriggerType    string              `json:"trigger_type,omitempty"`
	Source         string              `json:"source,omitempty"`
	DedupeKey      string              `json:"dedupe_key,omitempty"`
	Payload        map[string]any      `json:"payload,omitempty"`
	AttachmentIDs  []string            `json:"attachment_ids"`
	AttachmentURLs []ChatAttachmentURL `json:"attachment_urls"`
}

func (req loopTriggerHTTPRequest) ValidateRequest() error {
	return validatePromptPayload(req.Objective, req.AttachmentIDs, req.AttachmentURLs)
}

type loopGoalHTTPRequest struct {
	SessionID   string         `json:"session_id" validate:"notblank"`
	Objective   string         `json:"objective" validate:"notblank"`
	TemplateID  string         `json:"template_id,omitempty"`
	TaskType    string         `json:"task_type,omitempty"`
	Deliverable string         `json:"deliverable,omitempty"`
	Rubric      LoopRubric     `json:"rubric,omitempty"`
	Budget      LoopBudget     `json:"budget,omitempty"`
	Trigger     LoopTrigger    `json:"trigger,omitempty"`
	StopPolicy  LoopStopPolicy `json:"stop_policy,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func (req loopGoalHTTPRequest) ValidateRequest() error {
	if strings.TrimSpace(req.SessionID) == "" {
		return fmt.Errorf("session ID is required")
	}
	if strings.TrimSpace(req.Objective) == "" {
		return fmt.Errorf("objective is required")
	}
	return nil
}

func validatePromptPayload(content string, attachmentIDs []string, attachmentURLs []ChatAttachmentURL) error {
	if strings.TrimSpace(content) == "" && len(attachmentIDs) == 0 && len(attachmentURLs) == 0 {
		return fmt.Errorf("content or attachment is required")
	}
	return nil
}

func (req BrowserMemoryRequest) ValidateRequest() error {
	if strings.TrimSpace(req.URL) == "" && strings.TrimSpace(req.Title) == "" && strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("browser memory requires url, title, or content")
	}
	return nil
}

type memoryEpisodeSearchRequest struct {
	Query string `json:"query" validate:"notblank"`
	Limit int    `json:"limit,omitempty" validate:"gte=0"`
}

func (req memoryEpisodeSearchRequest) ValidateRequest() error {
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

type memoryEpisodePromoteRequest struct {
	EpisodeIDs []string `json:"episode_ids,omitempty"`
	Limit      int      `json:"limit,omitempty" validate:"gte=0"`
}

func (req EvaluationRunRequest) ValidateRequest() error {
	if strings.TrimSpace(req.Scope.UserID) == "" {
		return fmt.Errorf("scope.user_id is required")
	}
	return nil
}

package agentruntime

import (
	"context"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

const (
	agenticTaskWorkflowName    = "agentic_task"
	agenticTaskWorkflowVersion = "v1"
)

func agenticTaskWorkflowDefinition(timeout time.Duration) WorkflowDefinition {
	return WorkflowDefinition{
		Name:    agenticTaskWorkflowName,
		Version: agenticTaskWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "intent_router"},
			{Name: "retrieve_context"},
			{Name: "choose_tool_or_answer"},
			{Name: "execute_agent_turn", Timeout: timeout},
			{Name: "final_answer"},
		},
	}
}

func (r *Runtime) executeAgenticTaskWorkflow(ctx context.Context, req ChatRequest, session *state.Session, onToken func(string)) (runnerResult, error) {
	if r == nil {
		return runnerResult{}, nil
	}
	if r.workflowStore == nil {
		return r.run(ctx, req, session, onToken)
	}
	engine := NewWorkflowEngine(r.workflowStore, ContextWorkflowEventSink{})
	var result runnerResult
	var runErr error
	engine.RegisterStepHandler("intent_router", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		content := strings.TrimSpace(req.Content)
		intent := "chat"
		if strings.HasPrefix(content, "/") {
			intent = "skill"
		} else if len(req.AttachmentIDs) > 0 || len(req.AttachmentURLs) > 0 {
			intent = "attachment_task"
		}
		return map[string]any{
			"intent":           intent,
			"content_length":   len(content),
			"attachment_count": len(req.AttachmentIDs) + len(req.AttachmentURLs),
			"job_context":      jobIDFromContext(ctx) != "",
		}, nil
	})
	engine.RegisterStepHandler("retrieve_context", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		hiddenCount := 0
		for _, message := range session.Messages {
			if message.Hidden {
				hiddenCount++
			}
		}
		return map[string]any{
			"message_count":        len(session.Messages),
			"hidden_context_count": hiddenCount,
			"memory_configured":    r.memory != nil,
			"search_configured":    r.messageSearch != nil,
		}, nil
	})
	engine.RegisterStepHandler("choose_tool_or_answer", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		mode := "answer"
		if strings.HasPrefix(strings.TrimSpace(req.Content), "/") {
			mode = "skill"
		} else if len(req.AttachmentIDs) > 0 || len(req.AttachmentURLs) > 0 {
			mode = "model_with_attachments"
		}
		return map[string]any{
			"execution_mode":  mode,
			"max_turn_budget": "single_agent_turn",
		}, nil
	})
	engine.RegisterStepHandler("execute_agent_turn", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		result, runErr = r.run(ctx, req, session, onToken)
		output := map[string]any{
			"output_length": len(result.Output),
			"has_session":   result.Session != nil,
			"job_started":   result.Job != nil,
		}
		if result.Job != nil {
			output["job_id"] = result.Job.ID
			output["job_type"] = result.Job.Type
			output["job_reason"] = result.JobReason
		}
		return output, runErr
	})
	engine.RegisterStepHandler("final_answer", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		status := "answered"
		if result.Job != nil {
			status = "job_started"
		}
		return map[string]any{
			"final_status":  status,
			"output_length": len(result.Output),
		}, nil
	})
	_, err := engine.Execute(ctx, WorkflowRequest{
		Definition: agenticTaskWorkflowDefinition(r.config.TurnTimeout),
		UserID:     req.UserID,
		SessionID:  session.ID,
		JobID:      jobIDFromContext(ctx),
		State: map[string]any{
			"user_id":    req.UserID,
			"session_id": session.ID,
			"request_id": requestIDFromContext(ctx),
		},
	})
	if err != nil {
		return result, err
	}
	return result, runErr
}

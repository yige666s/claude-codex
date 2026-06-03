package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/state"
)

type RuntimeDeepAgentPlanner struct {
	runtime *Runtime
}

func NewRuntimeDeepAgentPlanner(runtime *Runtime) *RuntimeDeepAgentPlanner {
	return &RuntimeDeepAgentPlanner{runtime: runtime}
}

func (p *RuntimeDeepAgentPlanner) CreatePlan(ctx context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	if p == nil || p.runtime == nil {
		return DeepAgentPlan{}, fmt.Errorf("runtime deep agent planner is not configured")
	}
	runner := p.runtime.runnerForScope(Scope{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Prompt:    req.Goal,
	})
	plannerSession := state.NewSession("")
	prompt := deepAgentPlannerPrompt(req)
	result, err := runner.RunGeneratedPrompt(ctx, plannerSession, prompt)
	if err != nil {
		return DeepAgentPlan{}, err
	}
	plan, err := parseDeepAgentPlan(result.Output)
	if err != nil {
		schema := deepAgentPlanStructuredSchema()
		emitStructuredOutputValidationFailure(ctx, schema, "deep_agent_planner", ExtractAndValidateStructuredObject(result.Output, schema))
		repaired, repairErr := repairStructuredJSONWithRunner(ctx, runner, schema, result.Output, err, deepAgentPlanRepairContext(req))
		if repairErr == nil {
			plan, err = parseDeepAgentPlan(string(repaired))
		}
		if err != nil {
			fallbackPlan, fallbackErr := ruleDeepAgentPlanner{}.CreatePlan(ctx, req)
			if fallbackErr != nil {
				emitStructuredOutputFallbackEvent(ctx, schema, "deep_agent_planner", structuredFallbackRulePlanner, fallbackErr)
				return DeepAgentPlan{}, fmt.Errorf("deep agent planner output invalid after %s and %s: %w; repair failed: %v; fallback failed: %v", structuredFallbackRepairRetry, structuredFallbackRulePlanner, err, repairErr, fallbackErr)
			}
			emitStructuredOutputFallbackEvent(ctx, schema, "deep_agent_planner", structuredFallbackRulePlanner, nil)
			plan = fallbackPlan
		}
	}
	plan = normalizeDeepAgentPlan(req.Goal, plan)
	for _, step := range plan.Steps {
		if strings.TrimSpace(step.DoneCondition) == "" {
			return DeepAgentPlan{}, fmt.Errorf("deep agent planner returned step %q without done_condition", firstNonEmptyString(step.ID, step.Title))
		}
	}
	return plan, nil
}

func (p *RuntimeDeepAgentPlanner) NextAction(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	return ruleDeepAgentPlanner{}.NextAction(ctx, state, step)
}

type RuntimeDeepAgentExecutor struct {
	runtime *Runtime
}

func NewRuntimeDeepAgentExecutor(runtime *Runtime) *RuntimeDeepAgentExecutor {
	return &RuntimeDeepAgentExecutor{runtime: runtime}
}

func (e *RuntimeDeepAgentExecutor) ExecuteDeepAgentAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	if e == nil || e.runtime == nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "runtime deep agent executor is not configured"}, fmt.Errorf("runtime deep agent executor is not configured")
	}
	tool := strings.ToLower(strings.TrimSpace(action.Tool))
	switch tool {
	case "", "model", "answer", "llm":
		return e.executeModelAction(ctx, action, agentState)
	case "skill":
		return e.executeSkillAction(ctx, action, agentState)
	case "rag_search", "search", "message_search":
		return e.executeRAGSearchAction(ctx, action, agentState)
	default:
		return DeepAgentActionResult{
			Status: DeepAgentActionStatusFailed,
			Error:  fmt.Sprintf("unsupported deep agent tool: %s", action.Tool),
		}, fmt.Errorf("unsupported deep agent tool: %s", action.Tool)
	}
}

func (e *RuntimeDeepAgentExecutor) executeModelAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	userID := deepAgentActionString(action, "user_id")
	sessionID := deepAgentActionString(action, "session_id")
	if agentState != nil && agentState.WorkingMemory != nil {
		userID = firstNonEmptyString(userID, deepAgentWorkflowString(agentState.WorkingMemory, "user_id"))
		sessionID = firstNonEmptyString(sessionID, deepAgentWorkflowString(agentState.WorkingMemory, "session_id"))
	}
	prompt := firstNonEmptyString(
		deepAgentActionString(action, "prompt"),
		deepAgentActionString(action, "instruction"),
		deepAgentActionString(action, "query"),
		deepAgentActionString(action, "step_title"),
		action.StepID,
	)
	if strings.TrimSpace(prompt) == "" {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "model action prompt is required"}, fmt.Errorf("model action prompt is required")
	}
	session, err := e.deepAgentSession(ctx, userID, sessionID)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	if err := e.runtime.injectSessionRuntimeContexts(ctx, userID, session); err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	runner := e.runtime.runnerForScope(Scope{
		UserID:     userID,
		SessionID:  session.ID,
		WorkingDir: session.WorkingDir,
		Prompt:     prompt,
	})
	result, err := runner.RunGeneratedPrompt(ctx, session, prompt)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	if result.Session != nil && userID != "" && sessionID != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, result.Session); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true}, saveErr
		}
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    result.Output,
		Completed: true,
		Metadata: map[string]any{
			"tool":       "model",
			"session_id": session.ID,
		},
	}, nil
}

func (e *RuntimeDeepAgentExecutor) executeSkillAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	userID := deepAgentActionString(action, "user_id")
	sessionID := deepAgentActionString(action, "session_id")
	if agentState != nil && agentState.WorkingMemory != nil {
		userID = firstNonEmptyString(userID, deepAgentWorkflowString(agentState.WorkingMemory, "user_id"))
		sessionID = firstNonEmptyString(sessionID, deepAgentWorkflowString(agentState.WorkingMemory, "session_id"))
	}
	skillName := strings.TrimPrefix(firstNonEmptyString(deepAgentActionString(action, "skill"), deepAgentActionString(action, "skill_name")), "/")
	if skillName == "" {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "skill action skill_name is required"}, fmt.Errorf("skill action skill_name is required")
	}
	args := firstNonEmptyString(deepAgentActionString(action, "args"), deepAgentActionString(action, "input"))
	session, err := e.deepAgentSession(ctx, userID, sessionID)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	content := "/" + skillName
	if strings.TrimSpace(args) != "" {
		content += " " + args
	}
	result, err := e.runtime.runSkillCommand(withHiddenUserMessage(ctx), ChatRequest{
		UserID:    userID,
		SessionID: session.ID,
		Content:   content,
	}, userID, session, content, nil)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	if result.Session != nil && userID != "" && sessionID != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, result.Session); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true}, saveErr
		}
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    result.Output,
		Completed: result.Job == nil,
		Metadata: map[string]any{
			"tool":        "skill",
			"skill_name":  skillName,
			"session_id":  session.ID,
			"job_started": result.Job != nil,
		},
	}, nil
}

func (e *RuntimeDeepAgentExecutor) executeRAGSearchAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	userID := deepAgentActionString(action, "user_id")
	if agentState != nil && agentState.WorkingMemory != nil {
		userID = firstNonEmptyString(userID, deepAgentWorkflowString(agentState.WorkingMemory, "user_id"))
	}
	query := firstNonEmptyString(deepAgentActionString(action, "query"), deepAgentActionString(action, "prompt"), deepAgentActionString(action, "input"))
	if query == "" {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "rag_search action query is required"}, fmt.Errorf("rag_search action query is required")
	}
	limit := deepAgentActionInt(action, "limit", 5)
	results, err := e.runtime.SearchMessages(ctx, userID, query, limit, 0)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	data, _ := json.Marshal(results)
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    string(data),
		Completed: true,
		Metadata: map[string]any{
			"tool":         "rag_search",
			"query":        query,
			"result_count": len(results),
		},
	}, nil
}

func (e *RuntimeDeepAgentExecutor) deepAgentSession(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return state.NewSession(e.runtime.config.DefaultWorkingDir), nil
	}
	if e.runtime.sessions == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	session, err := e.runtime.sessions.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func deepAgentPlannerPrompt(req DeepAgentTaskRequest) string {
	return fmt.Sprintf(`You are the planner for a production DeepAgent controller.

Split the user goal into a small, executable plan. Return JSON only, with no markdown.

Rules:
- Use 1 to %d steps.
- Every step must have id, title, intent, done_condition, and metadata.
- metadata.tool must be one of: "model", "skill", "rag_search".
- For "model", metadata.args.prompt should contain the instruction.
- For "skill", metadata.args.skill_name and metadata.args.args should be present.
- For "rag_search", metadata.args.query should be present.
- Each done_condition must be concrete and verifiable.
- Do not include risky external side effects unless the goal explicitly requires them.

JSON shape:
{
  "goal": "string",
  "steps": [
    {
      "id": "step-1",
      "title": "string",
      "intent": "string",
      "done_condition": "string",
      "risk_level": "low|medium|high",
      "metadata": {
        "tool": "model|skill|rag_search",
        "args": {}
      }
    }
  ]
}

User goal:
%s`, normalizeDeepAgentPolicy(req.Policy).MaxSteps, strings.TrimSpace(req.Goal))
}

func deepAgentPlanRepairContext(req DeepAgentTaskRequest) string {
	return fmt.Sprintf("User goal: %s\nMax steps: %d", strings.TrimSpace(req.Goal), normalizeDeepAgentPolicy(req.Policy).MaxSteps)
}

func parseDeepAgentPlan(output string) (DeepAgentPlan, error) {
	result := ExtractAndValidateStructuredObject(output, deepAgentPlanStructuredSchema())
	if !result.Valid() {
		return DeepAgentPlan{}, result.Error()
	}
	jsonText, err := json.Marshal(result.Value)
	if err != nil {
		return DeepAgentPlan{}, err
	}
	var plan DeepAgentPlan
	if err := json.Unmarshal(jsonText, &plan); err != nil {
		return DeepAgentPlan{}, err
	}
	if err := validateDeepAgentPlanSemantics(plan); err != nil {
		return DeepAgentPlan{}, err
	}
	return plan, nil
}

func deepAgentPlanStructuredSchema() StructuredSchema {
	return StructuredSchema{
		Name:    "deep_agent_plan",
		Version: "v1",
		Schema: map[string]any{
			"type":     "object",
			"required": []string{"goal", "steps"},
			"properties": map[string]any{
				"goal": map[string]any{"type": "string"},
				"steps": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"id", "title", "done_condition", "metadata"},
						"properties": map[string]any{
							"id":             map[string]any{"type": "string"},
							"title":          map[string]any{"type": "string"},
							"intent":         map[string]any{"type": "string"},
							"status":         map[string]any{"type": "string"},
							"done_condition": map[string]any{"type": "string"},
							"risk_level":     map[string]any{"type": "string", "enum": []any{"", "low", "medium", "high"}},
							"metadata": map[string]any{
								"type":     "object",
								"required": []string{"tool", "args"},
								"properties": map[string]any{
									"tool": map[string]any{"type": "string", "enum": []any{"model", "skill", "rag_search"}},
									"args": map[string]any{"type": "object"},
								},
							},
						},
					},
				},
			},
		},
	}
}

func validateDeepAgentPlanSemantics(plan DeepAgentPlan) error {
	if strings.TrimSpace(plan.Goal) == "" {
		return fmt.Errorf("deep agent plan goal is required")
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("deep agent plan has no steps")
	}
	for idx, step := range plan.Steps {
		prefix := fmt.Sprintf("deep agent plan step %d", idx)
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("%s id is required", prefix)
		}
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("%s title is required", prefix)
		}
		if strings.TrimSpace(step.DoneCondition) == "" {
			return fmt.Errorf("%s done_condition is required", prefix)
		}
		tool := deepAgentWorkflowString(step.Metadata, "tool")
		args, _ := step.Metadata["args"].(map[string]any)
		switch tool {
		case "model":
			if deepAgentWorkflowString(args, "prompt") == "" {
				return fmt.Errorf("%s model args.prompt is required", prefix)
			}
		case "skill":
			if deepAgentWorkflowString(args, "skill_name") == "" {
				return fmt.Errorf("%s skill args.skill_name is required", prefix)
			}
		case "rag_search":
			if deepAgentWorkflowString(args, "query") == "" {
				return fmt.Errorf("%s rag_search args.query is required", prefix)
			}
		default:
			return fmt.Errorf("%s metadata.tool is unsupported: %s", prefix, tool)
		}
	}
	return nil
}

func extractDeepAgentJSONObject(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("deep agent planner returned empty output")
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err == nil && len(raw) > 0 {
		return string(raw), nil
	}
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("deep agent planner did not return a JSON object")
	}
	return output[start : end+1], nil
}

func deepAgentActionString(action DeepAgentAction, key string) string {
	if action.Args == nil {
		return ""
	}
	value, ok := action.Args[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func deepAgentActionInt(action DeepAgentAction, key string, fallback int) int {
	if action.Args == nil {
		return fallback
	}
	switch value := action.Args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}

func deepAgentWorkflowString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

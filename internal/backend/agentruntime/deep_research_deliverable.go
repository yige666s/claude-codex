package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	harnessstate "claude-codex/internal/harness/state"
)

const (
	DeepResearchDeliverableActionRespondInline  = "respond_inline"
	DeepResearchDeliverableActionCreateArtifact = "create_artifact"
)

var deepResearchFilenameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type RuntimeDeepResearchDeliverableDecider struct {
	runtime *Runtime
}

func NewRuntimeDeepResearchDeliverableDecider(runtime *Runtime) *RuntimeDeepResearchDeliverableDecider {
	return &RuntimeDeepResearchDeliverableDecider{runtime: runtime}
}

func (d *RuntimeDeepResearchDeliverableDecider) DecideDeepResearchDeliverable(ctx context.Context, req DeepAgentTaskRequest, state *DeepAgentState, run DeepResearchRunState, aggregate DeepResearchAggregateResult) (DeepResearchDeliverableDecision, error) {
	fallback := fallbackDeepResearchDeliverableDecision(req, state)
	if d == nil || d.runtime == nil || d.runtime.engineFactory == nil {
		return fallback, nil
	}
	runner := d.runtime.runnerForScope(Scope{
		UserID:       req.UserID,
		SessionID:    req.SessionID,
		Prompt:       req.Goal,
		AllowedTools: []string{"__no_tools_allowed__"},
	})
	if runner == nil {
		return fallback, nil
	}
	prompt := deepResearchDeliverableDecisionPrompt(req, state, run, aggregate)
	result, err := runner.RunGeneratedPrompt(ctx, harnessstate.NewSession(""), prompt)
	if err != nil {
		emitDeepResearchEvent(ctx, "deep_research_deliverable_decision_fallback", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research deliverable decision used fallback.", map[string]any{
			"event_group": "deep_research",
			"error":       err.Error(),
			"fallback":    fallback,
		})
		return fallback, nil
	}
	decision, err := parseDeepResearchDeliverableDecision(result.Output)
	if err != nil {
		emitDeepResearchEvent(ctx, "deep_research_deliverable_decision_fallback", req.SessionID, firstNonEmptyString(req.JobID, jobIDFromContext(ctx)), "Deep research deliverable decision used fallback.", map[string]any{
			"event_group": "deep_research",
			"error":       err.Error(),
			"fallback":    fallback,
		})
		return fallback, nil
	}
	return normalizeDeepResearchDeliverableDecision(decision, fallback), nil
}

type RuntimeDeepResearchArtifactPublisher struct {
	runtime *Runtime
}

func NewRuntimeDeepResearchArtifactPublisher(runtime *Runtime) *RuntimeDeepResearchArtifactPublisher {
	return &RuntimeDeepResearchArtifactPublisher{runtime: runtime}
}

func (p *RuntimeDeepResearchArtifactPublisher) PublishDeepResearchArtifact(ctx context.Context, req DeepAgentTaskRequest, _ *DeepAgentState, _ DeepResearchRunState, aggregate DeepResearchAggregateResult, decision DeepResearchDeliverableDecision) (DeepAgentArtifactRef, error) {
	if p == nil || p.runtime == nil {
		return DeepAgentArtifactRef{}, fmt.Errorf("runtime is not configured")
	}
	content := strings.TrimSpace(aggregate.FinalAnswer)
	if content == "" {
		return DeepAgentArtifactRef{}, fmt.Errorf("final answer is empty")
	}
	filename := deepResearchArtifactFilename(req, decision)
	contentType := firstNonEmptyString(strings.TrimSpace(decision.ContentType), "text/markdown")
	artifact, err := p.runtime.CreateArtifact(WithJobID(ctx, firstNonEmptyString(req.JobID, jobIDFromContext(ctx))), req.UserID, req.SessionID, filename, contentType, []byte(content))
	if err != nil {
		return DeepAgentArtifactRef{}, err
	}
	ref := deepAgentArtifactRefFromArtifact(artifact, "deep_research_aggregate")
	ref.StepID = "deep_research_aggregate"
	return ref, nil
}

func (c *DeepResearchController) decideDeepResearchDeliverable(ctx context.Context, req DeepAgentTaskRequest, state *DeepAgentState, run DeepResearchRunState, aggregate DeepResearchAggregateResult) (DeepResearchDeliverableDecision, error) {
	if c == nil || c.deliverableDecider == nil {
		return DeepResearchDeliverableDecision{Action: DeepResearchDeliverableActionRespondInline, Confidence: "medium", Reason: "no deliverable decider configured"}, nil
	}
	decision, err := c.deliverableDecider.DecideDeepResearchDeliverable(ctx, req, state, run, aggregate)
	if err != nil {
		return DeepResearchDeliverableDecision{}, err
	}
	return normalizeDeepResearchDeliverableDecision(decision, fallbackDeepResearchDeliverableDecision(req, state)), nil
}

func (c *DeepResearchController) publishDeepResearchArtifact(ctx context.Context, req DeepAgentTaskRequest, state *DeepAgentState, run DeepResearchRunState, aggregate DeepResearchAggregateResult, decision DeepResearchDeliverableDecision) (DeepAgentArtifactRef, error) {
	if c == nil || c.artifactPublisher == nil {
		return DeepAgentArtifactRef{}, fmt.Errorf("artifact publisher is not configured")
	}
	return c.artifactPublisher.PublishDeepResearchArtifact(ctx, req, state, run, aggregate, decision)
}

func deepResearchDecisionRequiresArtifact(decision DeepResearchDeliverableDecision) bool {
	return decision.RequiresArtifact || strings.EqualFold(strings.TrimSpace(decision.Action), DeepResearchDeliverableActionCreateArtifact)
}

func normalizeDeepResearchDeliverableDecision(decision, fallback DeepResearchDeliverableDecision) DeepResearchDeliverableDecision {
	action := strings.TrimSpace(decision.Action)
	if action == "" {
		action = fallback.Action
	}
	switch strings.ToLower(action) {
	case DeepResearchDeliverableActionCreateArtifact, "artifact", "file", "create_file":
		decision.Action = DeepResearchDeliverableActionCreateArtifact
		decision.RequiresArtifact = true
	default:
		decision.Action = DeepResearchDeliverableActionRespondInline
		decision.RequiresArtifact = false
	}
	decision.DeliverableType = firstNonEmptyString(strings.TrimSpace(decision.DeliverableType), fallback.DeliverableType, "markdown")
	decision.FilenameHint = firstNonEmptyString(strings.TrimSpace(decision.FilenameHint), fallback.FilenameHint)
	decision.ContentType = firstNonEmptyString(strings.TrimSpace(decision.ContentType), fallback.ContentType, "text/markdown")
	decision.Reason = firstNonEmptyString(strings.TrimSpace(decision.Reason), fallback.Reason)
	decision.Confidence = firstNonEmptyString(strings.TrimSpace(decision.Confidence), fallback.Confidence, "medium")
	return decision
}

func fallbackDeepResearchDeliverableDecision(req DeepAgentTaskRequest, agentState *DeepAgentState) DeepResearchDeliverableDecision {
	contract := req.LoopContract
	if contract.ID == "" && agentState != nil && agentState.LoopContract.ID != "" {
		contract = agentState.LoopContract
	}
	if contract.ID == "" && req.State != nil {
		contract = loopContractFromWorkflowValue(req.State["loop_contract"])
	}
	decision := DeepResearchDeliverableDecision{
		Action:          DeepResearchDeliverableActionRespondInline,
		DeliverableType: firstNonEmptyString(contract.Deliverable.Type, "markdown"),
		FilenameHint:    firstNonEmptyString(contract.Deliverable.FilenameHint, "deep-research-report.md"),
		ContentType:     deepResearchContentTypeForDeliverable(contract.Deliverable),
		Reason:          "fallback chose inline response because no explicit artifact action was available",
		Confidence:      "low",
	}
	if len(contract.EvaluatorPolicy.ArtifactRequired) > 0 || len(contract.Rubric.RequiredArtifacts) > 0 || loopContractDeliverableRequiresArtifact(contract.Deliverable) {
		decision.Action = DeepResearchDeliverableActionCreateArtifact
		decision.RequiresArtifact = true
		decision.Reason = "fallback contract requires a generated artifact"
	}
	return decision
}

func deepResearchDeliverableDecisionPrompt(req DeepAgentTaskRequest, state *DeepAgentState, run DeepResearchRunState, aggregate DeepResearchAggregateResult) string {
	contract := req.LoopContract
	if contract.ID == "" && state != nil {
		contract = state.LoopContract
	}
	contractJSON, _ := json.MarshalIndent(contract, "", "  ")
	var b strings.Builder
	b.WriteString("You are the final delivery controller for a Deep Research run.\n")
	b.WriteString("Choose exactly one application delivery tool:\n")
	b.WriteString("- respond_inline: return the final answer in the chat only.\n")
	b.WriteString("- create_artifact: create a downloadable artifact from the final answer.\n")
	b.WriteString("Return JSON only. Do not include markdown fences.\n\n")
	b.WriteString("Use action=create_artifact only when the user's objective or loop contract explicitly asks for a generated report, document, file, artifact, export, downloadable deliverable, or required artifact.\n")
	b.WriteString("Use action=respond_inline for ordinary research, Q&A, summaries, or analysis where the user did not ask to generate a file/document/report artifact.\n\n")
	b.WriteString("Important: an inferred loop contract deliverable such as type=report format=markdown is an internal answer shape, not by itself a downloadable artifact request.\n")
	b.WriteString("If the user only asks to research, investigate, summarize, or analyze, choose respond_inline even when the inferred deliverable is report/markdown.\n")
	b.WriteString("Do not choose create_artifact merely because the user asks for bullet points, key points, sources, citations, tables, structured findings, markdown, or a final answer.\n")
	b.WriteString("Choose create_artifact for report/markdown only when the user explicitly asks to generate, create, export, download, or produce a report/document/file, or when required_artifacts/artifact_required is set.\n\n")
	b.WriteString("User objective:\n")
	b.WriteString(strings.TrimSpace(req.Goal))
	b.WriteString("\n\nLoop contract:\n")
	b.Write(contractJSON)
	b.WriteString("\n\nAggregate summary:\n")
	b.WriteString(truncateDeepAgentDiagnosticText(firstNonEmptyString(aggregate.Summary, aggregate.FinalAnswer), 1200))
	b.WriteString("\n\nSchema:\n")
	b.WriteString(`{"action":"respond_inline|create_artifact","requires_artifact":true|false,"deliverable_type":"markdown|docx|pdf|answer|report","filename_hint":"optional filename","content_type":"text/markdown|application/json|...","reason":"short reason","confidence":"low|medium|high"}`)
	return b.String()
}

func parseDeepResearchDeliverableDecision(output string) (DeepResearchDeliverableDecision, error) {
	schema := deepResearchDeliverableDecisionStructuredSchema()
	result := ExtractAndValidateStructuredObject(output, schema)
	if !result.Valid() {
		return DeepResearchDeliverableDecision{}, result.Error()
	}
	data, err := json.Marshal(result.Value)
	if err != nil {
		return DeepResearchDeliverableDecision{}, err
	}
	var decision DeepResearchDeliverableDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		return DeepResearchDeliverableDecision{}, err
	}
	return decision, nil
}

func deepResearchDeliverableDecisionStructuredSchema() StructuredSchema {
	return StructuredSchema{
		Name:    "deep_research_deliverable_decision",
		Version: "v1",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": true,
			"required":             []any{"action", "requires_artifact"},
			"properties": map[string]any{
				"action":            map[string]any{"type": "string"},
				"requires_artifact": map[string]any{"type": "boolean"},
				"deliverable_type":  map[string]any{},
				"filename_hint":     map[string]any{},
				"content_type":      map[string]any{},
				"reason":            map[string]any{},
				"confidence":        map[string]any{},
			},
		},
	}
}

func deepResearchArtifactFilename(req DeepAgentTaskRequest, decision DeepResearchDeliverableDecision) string {
	name := strings.TrimSpace(decision.FilenameHint)
	if name == "" {
		name = "deep-research-report.md"
	}
	name = filepath.Base(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	stem = strings.Trim(deepResearchFilenameUnsafe.ReplaceAllString(stem, "-"), "-_.")
	if stem == "" {
		stem = "deep-research-report"
	}
	if ext == "" {
		ext = deepResearchExtensionForDecision(decision)
	}
	return stem + ext
}

func deepResearchExtensionForDecision(decision DeepResearchDeliverableDecision) string {
	switch strings.ToLower(strings.TrimSpace(decision.DeliverableType)) {
	case "json":
		return ".json"
	case "docx":
		return ".docx"
	case "pdf":
		return ".pdf"
	default:
		return ".md"
	}
}

func deepResearchContentTypeForDeliverable(deliverable LoopContractDeliverable) string {
	switch strings.ToLower(strings.TrimSpace(deliverable.Format)) {
	case "json":
		return "application/json"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "pdf":
		return "application/pdf"
	default:
		return "text/markdown"
	}
}

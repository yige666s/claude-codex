package agentruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func loopContractIDFromJobID(jobID string) string {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return ""
	}
	return "loop-" + jobID
}

func BuildLoopContractFromDeepAgentRequest(req DeepAgentTaskRequest, fallbackJobID, requestID string, now time.Time) LoopContract {
	contract := req.LoopContract
	goal := strings.TrimSpace(req.Goal)
	if contract.Objective == "" {
		contract.Objective = goal
	}
	if contract.Version == "" {
		contract.Version = LoopContractVersion
	}
	if contract.ID == "" {
		contract.ID = firstNonEmptyString(
			deepAgentWorkflowString(req.State, "loop_contract_id"),
			deepAgentWorkflowString(req.State, "loop_goal_id"),
			loopContractIDFromJobID(req.JobID),
			loopContractIDFromJobID(fallbackJobID),
			loopContractIDFromJobID(requestID),
		)
	}
	if contract.ID == "" {
		contract.ID = hashedLoopContractID(req.UserID, req.SessionID, contract.Objective)
	}
	if contract.TaskType == "" {
		contract.TaskType = inferLoopContractTaskType(contract.Objective)
	}
	if contract.Deliverable.Type == "" && contract.Deliverable.Format == "" {
		contract.Deliverable = inferLoopContractDeliverable(contract.Objective)
	}
	if deepAgentRubricEmpty(contract.Rubric) {
		contract.Rubric = normalizeDeepAgentRubric(req.Rubric)
	}
	if contract.Budget.MaxSteps == 0 {
		contract.Budget.MaxSteps = req.Policy.MaxSteps
	}
	if contract.Budget.MaxActions == 0 {
		contract.Budget.MaxActions = req.Policy.MaxActions
	}
	if contract.Budget.MaxDurationMS == 0 {
		contract.Budget.MaxDurationMS = req.Policy.MaxDuration.Milliseconds()
	}
	if contract.Budget.StepTimeoutMS == 0 {
		contract.Budget.StepTimeoutMS = req.Policy.StepTimeout.Milliseconds()
	}
	if contract.Budget.NoProgressLimit == 0 {
		contract.Budget.NoProgressLimit = req.Policy.NoProgressLimit
	}
	if len(contract.ToolPolicy.AllowedModes) == 0 {
		contract.ToolPolicy.AllowedModes = []string{
			DeepAgentToolModeModel,
			DeepAgentToolModeWeb,
			DeepAgentToolModeModelArtifact,
			DeepAgentToolModeSkill,
			DeepAgentToolModeConnector,
			DeepAgentToolModeMulti,
			DeepAgentToolModeRAGSearch,
			DeepAgentToolModeTest,
			DeepAgentToolModeCodePatch,
		}
	}
	if len(contract.ToolPolicy.ConnectorContext) == 0 {
		contract.ToolPolicy.ConnectorContext = normalizeConnectorScopes(req.ConnectorContext)
	}
	if contract.ToolPolicy.WriteMode == "" {
		contract.ToolPolicy.WriteMode = "review_required"
	}
	if contract.SourcePolicy.QualityBar == "" {
		contract.SourcePolicy.QualityBar = firstNonEmptyString(contract.Rubric.QualityBar, "traceable and relevant evidence")
	}
	stateSourcePolicy := deepAgentSourcePolicyFromAny(req.State["source_policy"])
	if contract.SourcePolicy.MaxSourcesPerBranch <= 0 {
		contract.SourcePolicy.MaxSourcesPerBranch = stateSourcePolicy.MaxSourcesPerBranch
	}
	if contract.SourcePolicy.MinSourceScore <= 0 {
		contract.SourcePolicy.MinSourceScore = stateSourcePolicy.MinSourceScore
	}
	if !contract.SourcePolicy.RequiresSources {
		contract.SourcePolicy.RequiresSources = loopContractRequiresSources(contract.Objective, contract.Rubric)
	}
	if contract.SourcePolicy.RequiresSources && contract.SourcePolicy.MinSourceCount == 0 {
		contract.SourcePolicy.MinSourceCount = 3
	}
	contract.SourcePolicy = normalizeLoopContractSourcePolicy(contract.SourcePolicy, contract.Objective)
	if len(contract.RiskPolicy.ForbiddenActions) == 0 {
		contract.RiskPolicy.ForbiddenActions = append([]string(nil), contract.Rubric.ForbiddenActions...)
	}
	if contract.RiskPolicy.ReviewPolicy == "" {
		mode := loopContractNestedString(req.State, "deep_agent_governance", "risky_write_approval_mode")
		if mode == "" {
			mode = loopContractNestedString(req.State, "deep_agent_governance", "high_risk_policy")
		}
		switch normalizeRiskyWriteApprovalMode(mode) {
		case "allow":
			contract.RiskPolicy.ReviewPolicy = "allow risky write actions"
		case "block":
			contract.RiskPolicy.ReviewPolicy = "block risky write actions"
			if !deepAgentStringSliceContains(contract.RiskPolicy.ForbiddenActions, "risky_write") {
				contract.RiskPolicy.ForbiddenActions = append(contract.RiskPolicy.ForbiddenActions, "risky_write")
			}
		default:
			contract.RiskPolicy.ReviewPolicy = "review risky write actions before execution"
		}
	}
	if len(contract.StopPolicy.DoneWhen) == 0 {
		contract.StopPolicy.DoneWhen = loopContractDoneConditions(contract)
	}
	if contract.StopPolicy.MaxNoProgress == 0 {
		contract.StopPolicy.MaxNoProgress = req.Policy.NoProgressLimit
	}
	if contract.StopPolicy.OnBudgetExceeded == "" {
		contract.StopPolicy.OnBudgetExceeded = "stop and report partial progress with blocker"
	}
	if contract.EvaluatorPolicy.Verifier == "" {
		contract.EvaluatorPolicy.Verifier = "deep_agent_final_verifier"
	}
	if contract.EvaluatorPolicy.TimeoutMS == 0 {
		contract.EvaluatorPolicy.TimeoutMS = loopContractNestedInt64(req.State, "deep_agent_governance", "evaluator_timeout_ms")
	}
	if contract.EvaluatorPolicy.ConflictTimeoutMS == 0 {
		contract.EvaluatorPolicy.ConflictTimeoutMS = loopContractNestedInt64(req.State, "deep_agent_governance", "conflict_reconciliation_timeout_ms")
	}
	if !contract.EvaluatorPolicy.RequiresFinalVerification {
		contract.EvaluatorPolicy.RequiresFinalVerification = true
	}
	if len(contract.EvaluatorPolicy.EvidenceRequired) == 0 {
		contract.EvaluatorPolicy.EvidenceRequired = append([]string(nil), contract.Rubric.RequiredEvidence...)
	}
	if len(contract.EvaluatorPolicy.ArtifactRequired) == 0 {
		contract.EvaluatorPolicy.ArtifactRequired = append([]string(nil), contract.Rubric.RequiredArtifacts...)
	}
	if contract.CreatedFrom == "" {
		contract.CreatedFrom = "deep_agent_request"
	}
	if contract.CreatedAt.IsZero() {
		if now.IsZero() {
			now = time.Now().UTC()
		}
		contract.CreatedAt = now.UTC()
	}
	return contract
}

func loopContractNestedString(values map[string]any, group, key string) string {
	if values == nil {
		return ""
	}
	raw, _ := values[group].(map[string]any)
	if len(raw) == 0 {
		return ""
	}
	return deepAgentWorkflowString(raw, key)
}

func loopContractNestedInt64(values map[string]any, group, key string) int64 {
	if values == nil {
		return 0
	}
	raw, _ := values[group].(map[string]any)
	if len(raw) == 0 {
		return 0
	}
	value := raw[key]
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		n, _ := typed.Int64()
		return n
	case string:
		var n int64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &n); err == nil {
			return n
		}
	}
	return 0
}

func loopContractFromWorkflowValue(value any) LoopContract {
	if value == nil {
		return LoopContract{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return LoopContract{}
	}
	var contract LoopContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return LoopContract{}
	}
	if contract.Version == "" && contract.ID != "" {
		contract.Version = LoopContractVersion
	}
	return contract
}

func hydrateLoopContractWorkingMemory(memory map[string]any, contract LoopContract) {
	if memory == nil || contract.ID == "" {
		return
	}
	memory["loop_contract"] = contract
	memory["loop_contract_id"] = contract.ID
	memory["loop_contract_version"] = contract.Version
	memory["loop_goal_id"] = firstNonEmptyString(deepAgentWorkflowString(memory, "loop_goal_id"), contract.ID)
	memory["loop_task_type"] = contract.TaskType
	if loopContractDeliverableRequiresArtifact(contract.Deliverable) {
		memory["deliverable"] = contract.Deliverable.Type
	} else {
		delete(memory, "deliverable")
	}
	if contract.Deliverable.Type != "" {
		memory["deliverable_type"] = contract.Deliverable.Type
	}
	if contract.Deliverable.Format != "" {
		memory["deliverable_format"] = contract.Deliverable.Format
	}
	memory["source_policy"] = contract.SourcePolicy
	memory["stop_policy"] = contract.StopPolicy
}

func loopContractDeliverableRequiresArtifact(deliverable LoopContractDeliverable) bool {
	format := strings.ToLower(strings.TrimSpace(deliverable.Format))
	kind := strings.ToLower(strings.TrimSpace(deliverable.Type))
	if kind == "document" || kind == "image" {
		return true
	}
	return deepAgentContainsAny(format, "docx", "xlsx", "pptx", "png", "jpg", "jpeg", "svg", "pdf")
}

func hashedLoopContractID(parts ...string) string {
	joined := strings.Join(parts, "\n")
	if strings.TrimSpace(joined) == "" {
		joined = time.Now().UTC().Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(joined))
	return "loop-" + hex.EncodeToString(sum[:])[:24]
}

func inferLoopContractTaskType(goal string) string {
	lower := strings.ToLower(goal)
	switch {
	case strings.Contains(lower, "multi-agent") || strings.Contains(goal, "多智能体") || strings.Contains(goal, "并行"):
		return "multi_agent_research"
	case strings.Contains(goal, "调研") || strings.Contains(goal, "研究") || strings.Contains(lower, "research") || strings.Contains(lower, "investigate"):
		return "research_report"
	case strings.Contains(goal, "报告") || strings.Contains(lower, "report"):
		return "report_generation"
	case strings.Contains(goal, "图片") || strings.Contains(goal, "生图") || strings.Contains(lower, "image"):
		return "image_generation"
	case strings.Contains(lower, "word") || strings.Contains(lower, "docx") || strings.Contains(goal, "文档"):
		return "document_generation"
	default:
		return "general_task"
	}
}

func inferLoopContractDeliverable(goal string) LoopContractDeliverable {
	lower := strings.ToLower(goal)
	switch {
	case strings.Contains(lower, "word") || strings.Contains(lower, "docx"):
		return LoopContractDeliverable{Type: "document", Format: "docx", FilenameHint: "report.docx"}
	case strings.Contains(goal, "报告") || strings.Contains(goal, "调研") || strings.Contains(goal, "研究") || strings.Contains(lower, "report") || strings.Contains(lower, "research"):
		return LoopContractDeliverable{Type: "report", Format: "markdown", FilenameHint: "report.md"}
	case strings.Contains(goal, "图片") || strings.Contains(goal, "生图") || strings.Contains(lower, "image"):
		return LoopContractDeliverable{Type: "image", Format: "image"}
	default:
		return LoopContractDeliverable{Type: "answer", Format: "markdown"}
	}
}

func loopContractRequiresSources(goal string, rubric DeepAgentRubric) bool {
	if len(rubric.RequiredEvidence) > 0 {
		return true
	}
	lower := strings.ToLower(goal)
	keywords := []string{"research", "investigate", "current", "external", "citation", "source"}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	for _, keyword := range []string{"调研", "研究", "引用", "来源", "当前", "外部"} {
		if strings.Contains(goal, keyword) {
			return true
		}
	}
	return false
}

func loopContractDoneConditions(contract LoopContract) []string {
	done := append([]string(nil), contract.Rubric.AcceptanceCriteria...)
	if len(contract.Rubric.RequiredEvidence) > 0 || contract.SourcePolicy.RequiresSources {
		done = append(done, "required evidence and sources are attached")
	}
	if len(contract.Rubric.RequiredArtifacts) > 0 {
		done = append(done, "required artifacts are created and referenced")
	}
	if len(done) == 0 {
		done = append(done, "final answer satisfies the user objective")
	}
	return done
}

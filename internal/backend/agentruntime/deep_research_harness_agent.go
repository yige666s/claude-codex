package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

const deepResearchHarnessAgentOutputSchema = `Return a single JSON object with this shape:
{
  "summary": "one concise paragraph",
  "output": "worker notes or markdown",
  "findings": [{"claim":"...", "evidence":"...", "source_url":"...", "confidence":"high|medium|low"}],
  "sources": [{"url":"https://... or empty for a local file", "title":"URL title or repository file path", "snippet":"...", "provider":"WebSearch|WebFetch|Read|Grep"}],
  "open_questions": ["..."]
}
Only list sources that were actually returned by a research tool in this run; never infer or invent a citation.
If a tool is unavailable, explain that limitation in output.`

var deepResearchHarnessURLPattern = regexp.MustCompile(`https?://[^\s<>"')\]]+`)

// EngineDeepResearchHarnessAgentRunner adapts Deep Research worker nodes to the
// AgentAPI engine factory. The factory is the production bridge into the harness
// planner/tool runtime, so child workers inherit connector tools, permissions,
// sandboxing, artifacts, and LLM governance instead of falling back to inline
// orchestration.
type EngineDeepResearchHarnessAgentRunner struct {
	runtime *Runtime
}

func NewEngineDeepResearchHarnessAgentRunner(runtime *Runtime) *EngineDeepResearchHarnessAgentRunner {
	return &EngineDeepResearchHarnessAgentRunner{runtime: runtime}
}

func (r *EngineDeepResearchHarnessAgentRunner) RunDeepResearchAgent(ctx context.Context, input DeepResearchWorkerInput) (DeepResearchWorkerResult, error) {
	if r == nil || r.runtime == nil {
		return DeepResearchWorkerResult{}, fmt.Errorf("deep research harness agent runner is not configured")
	}
	input = cloneDeepResearchWorkerInput(input)
	agentRunID := deepResearchAgentRunID(input)
	childSession := deepResearchChildSession(r.runtime, input, agentRunID)
	scope := Scope{
		UserID:            input.UserID,
		SessionID:         input.SessionID,
		WorkingDir:        childSession.WorkingDir,
		Prompt:            input.Goal,
		InternalToolScope: true,
		AllowedTools:      deepResearchHarnessAllowedTools(input.Node.AllowedTools),
		ConnectorContext:  append([]string(nil), input.ConnectorContext...),
		ArtifactTypes:     []string{"text/markdown", "application/json", "text/plain"},
	}
	runner := r.runtime.runnerForScope(ctx, scope)
	if runner == nil {
		return DeepResearchWorkerResult{}, fmt.Errorf("deep research harness agent runner factory returned nil")
	}

	prompt := deepResearchHarnessAgentPrompt(input)
	streamedChars := 0
	nextProgressAt := 1200
	result, err := runWithTokenStream(ctx, runner, childSession, prompt, true, func(token string) {
		streamedChars += len(token)
		if streamedChars >= nextProgressAt {
			nextProgressAt += 1200
			emitDeepResearchEvent(ctx, "deep_research_worker_progress", input.SessionID, input.JobID, "Deep research worker is streaming output.", map[string]any{
				"node_id":      input.Node.ID,
				"agent_run_id": agentRunID,
				"stream_chars": streamedChars,
			})
		}
	})

	out := deepResearchWorkerResultFromHarnessOutput(input.Node, result, err)
	out.AgentRunID = firstNonEmptyString(out.AgentRunID, agentRunID)
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	out.Metadata["worker_backend"] = DeepResearchWorkerBackendHarnessAgent
	out.Metadata["runner"] = "engine_factory"
	out.Metadata["agent_run_id"] = agentRunID
	out.Metadata["child_session_id"] = childSession.ID
	out.Metadata["child_parent_session_id"] = input.SessionID
	out.Metadata["allowed_tools"] = scope.AllowedTools
	out.Metadata["stream_chars"] = streamedChars
	out.Metadata["output_chars"] = len(strings.TrimSpace(result.Output))
	return out, err
}

func deepResearchChildSession(runtime *Runtime, input DeepResearchWorkerInput, agentRunID string) *state.Session {
	workingDir := ""
	if runtime != nil {
		workingDir = runtime.config.DefaultWorkingDir
	}
	if wd := deepAgentWorkflowString(input.WorkingMemory, "working_dir"); strings.TrimSpace(wd) != "" {
		workingDir = wd
	}
	session := state.NewSession(workingDir)
	session.ID = agentRunID
	session.UserID = input.UserID
	session.ParentID = input.SessionID
	session.Description = firstNonEmptyString(input.Node.Title, input.Node.ID)
	session.Tags = []string{"deep_research", "subagent", firstNonEmptyString(input.Node.WorkerRole, "worker")}
	session.Metadata = map[string]string{
		"job_id":             input.JobID,
		"parent_session_id":  input.SessionID,
		"deep_research_node": input.Node.ID,
		"worker_role":        input.Node.WorkerRole,
		"worker_backend":     DeepResearchWorkerBackendHarnessAgent,
	}
	return session
}

func deepResearchAgentRunID(input DeepResearchWorkerInput) string {
	node := strings.TrimSpace(input.Node.ID)
	if node == "" {
		node = "worker"
	}
	node = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, node)
	attempt := maxInt(input.Node.Attempt, 1)
	return fmt.Sprintf("drw-%s-a%d-%s", node, attempt, newSortableID())
}

func deepResearchHarnessAllowedTools(in []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, tool := range in {
		switch strings.ToLower(strings.TrimSpace(tool)) {
		case "", "model", "none":
		case "repo_search", "repo-search", "code_search", "code-search":
			add("Read")
			add("Glob")
			add("Grep")
		case "test", "tests", "shell", "bash":
			add("Bash")
		case "artifact", "artifacts":
			add(ArtifactToolName)
		default:
			add(tool)
		}
	}
	if len(out) == 0 {
		return []string{"__no_tools_allowed__"}
	}
	return out
}

func deepResearchHarnessAgentPrompt(input DeepResearchWorkerInput) string {
	base := deepResearchWorkerPrompt(input)
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n")
	b.WriteString(deepResearchHarnessAgentOutputSchema)
	if len(input.ConnectorContext) > 0 {
		b.WriteString("\n\nConnector context:")
		for _, line := range input.ConnectorContext {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				b.WriteString("\n- ")
				b.WriteString(truncateDeepAgentDiagnosticText(trimmed, 500))
			}
		}
	}
	return b.String()
}

func deepResearchWorkerResultFromHarnessOutput(node DeepResearchTaskNode, result engine.Result, runErr error) DeepResearchWorkerResult {
	text := strings.TrimSpace(result.Output)
	out := DeepResearchWorkerResult{
		Status:   DeepAgentActionStatusSucceeded,
		Output:   text,
		Summary:  truncateDeepAgentDiagnosticText(firstNonEmptyString(text, node.Title), 800),
		Metadata: map[string]any{"output_format": "text"},
	}
	if runErr != nil {
		out.Status = DeepAgentActionStatusFailed
		out.Errors = append(out.Errors, runErr.Error())
		out.Summary = truncateDeepAgentDiagnosticText(firstNonEmptyString(text, runErr.Error(), node.Title), 800)
	}
	if parsed, ok := parseDeepResearchHarnessJSON(text); ok {
		out.Metadata["output_format"] = "json"
		out.Summary = firstNonEmptyString(parsed.Summary, out.Summary)
		out.Output = firstNonEmptyString(parsed.Output, text)
		out.Findings = append(out.Findings, parsed.Findings...)
		out.Sources = append(out.Sources, parsed.Sources...)
		out.Artifacts = append(out.Artifacts, parsed.Artifacts...)
		out.ToolCalls = append(out.ToolCalls, parsed.ToolCalls...)
		out.OpenQuestions = append(out.OpenQuestions, parsed.OpenQuestions...)
		out.Errors = append(out.Errors, parsed.Errors...)
	}
	out.Sources = append(out.Sources, deepResearchSourcesFromText(text)...)
	out.ToolCalls = deepResearchHarnessToolCalls(result.Session)
	for idx := range out.Sources {
		if out.Sources[idx].SourceKind == "unverified_model_text" {
			continue
		}
		if deepResearchHarnessSourceBackedByToolResult(out.Sources[idx], result.Session) {
			out.Sources[idx].SourceKind = "tool_verified"
		} else {
			out.Sources[idx].SourceKind = "unverified_model_report"
		}
	}
	out.Sources = dedupeDeepResearchSources(out.Sources)
	out.Artifacts = dedupeDeepResearchArtifacts(out.Artifacts)
	if len(out.Findings) == 0 {
		finding := DeepResearchFinding{
			Claim:      firstNonEmptyString(out.Summary, node.Title),
			Evidence:   truncateDeepAgentDiagnosticText(firstNonEmptyString(out.Output, out.Summary), 1000),
			Confidence: "medium",
		}
		if len(out.Sources) > 0 {
			finding.SourceURL = out.Sources[0].URL
		}
		out.Findings = []DeepResearchFinding{finding}
	}
	return out
}

func deepResearchHarnessToolCalls(session *state.Session) []DeepAgentToolCallRef {
	if session == nil {
		return nil
	}
	byID := map[string]int{}
	out := make([]DeepAgentToolCallRef, 0)
	for _, message := range session.Messages {
		for _, call := range message.ToolCalls {
			id := strings.TrimSpace(call.ID)
			if id == "" {
				continue
			}
			if _, exists := byID[id]; exists {
				continue
			}
			byID[id] = len(out)
			out = append(out, DeepAgentToolCallRef{ID: id, Name: call.Name, Status: "called"})
		}
		if message.Role != "tool" || strings.TrimSpace(message.ToolCallID) == "" {
			continue
		}
		idx, exists := byID[message.ToolCallID]
		if !exists {
			byID[message.ToolCallID] = len(out)
			out = append(out, DeepAgentToolCallRef{ID: message.ToolCallID, Name: message.ToolName, Status: "result"})
			continue
		}
		out[idx].Name = firstNonEmptyString(message.ToolName, out[idx].Name)
		out[idx].Status = "result"
	}
	return out
}

func deepResearchHarnessSourceBackedByToolResult(source DeepAgentSourceRef, session *state.Session) bool {
	if session == nil {
		return false
	}
	urlNeedle := strings.TrimSpace(source.URL)
	titleNeedle := strings.TrimSpace(source.Title)
	for _, message := range session.Messages {
		if message.Role != "tool" || !deepResearchToolNameCanVerifySources(message.ToolName) {
			continue
		}
		evidenceText := strings.TrimSpace(message.ToolOutput + "\n" + string(message.ToolInput))
		if evidenceText == "" {
			continue
		}
		if urlNeedle != "" && strings.Contains(evidenceText, urlNeedle) {
			return true
		}
		if urlNeedle == "" && len(titleNeedle) >= 3 && strings.Contains(strings.ToLower(evidenceText), strings.ToLower(titleNeedle)) {
			return true
		}
	}
	return false
}

func deepResearchToolNameCanVerifySources(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "websearch", "webfetch", "read", "glob", "grep", "repo_search", "repo-search", "code_search", "code-search", "bash", "test":
		return true
	default:
		return false
	}
}

func parseDeepResearchHarnessJSON(text string) (DeepResearchWorkerResult, bool) {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return DeepResearchWorkerResult{}, false
	}
	if strings.HasPrefix(candidate, "```") {
		candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "```json"))
		candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "```"))
		candidate = strings.TrimSpace(strings.TrimSuffix(candidate, "```"))
	}
	if !strings.HasPrefix(candidate, "{") {
		start := strings.Index(candidate, "{")
		end := strings.LastIndex(candidate, "}")
		if start < 0 || end <= start {
			return DeepResearchWorkerResult{}, false
		}
		candidate = candidate[start : end+1]
	}
	var out DeepResearchWorkerResult
	if err := json.Unmarshal([]byte(candidate), &out); err != nil {
		return DeepResearchWorkerResult{}, false
	}
	return out, true
}

func deepResearchSourcesFromText(text string) []DeepAgentSourceRef {
	matches := deepResearchHarnessURLPattern.FindAllString(text, -1)
	out := make([]DeepAgentSourceRef, 0, len(matches))
	seen := map[string]bool{}
	for _, raw := range matches {
		raw = strings.TrimRight(raw, ".,;:!?")
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, DeepAgentSourceRef{
			URL:        raw,
			Title:      raw,
			Provider:   "model_text",
			SourceKind: "unverified_model_text",
		})
	}
	return out
}

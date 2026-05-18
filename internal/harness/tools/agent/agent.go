package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	coreagent "claude-codex/internal/harness/agent"
	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/permissions"
	coretasks "claude-codex/internal/harness/tasks"
	toolkit "claude-codex/internal/harness/tools"
)

type Request struct {
	Prompt           string            `json:"prompt"`
	Description      string            `json:"description,omitempty"`
	SubagentType     string            `json:"subagent_type,omitempty"`
	Model            string            `json:"model,omitempty"`
	Cwd              string            `json:"cwd,omitempty"`
	WorkingDir       string            `json:"working_dir,omitempty"`
	Name             string            `json:"name,omitempty"`
	TeamName         string            `json:"team_name,omitempty"`
	Mode             string            `json:"mode,omitempty"`
	RunInBackground  bool              `json:"run_in_background,omitempty"`
	Isolation        string            `json:"isolation,omitempty"`
	MaxTurns         int               `json:"max_turns,omitempty"`
	InvocationKind   string            `json:"-"`
	WorktreePath     string            `json:"-"`
	WorktreeBranch   string            `json:"-"`
	ParentWorkDir    string            `json:"-"`
	AgentID          string            `json:"-"`
	ParentAgentID    string            `json:"-"`
	ParentSessionID  string            `json:"-"`
	ParentMetadata   map[string]string `json:"-"`
	ParentMessages   []string          `json:"-"`
	DefinitionSource string            `json:"-"`
	DefinitionMemory string            `json:"-"`
	PermissionPolicy string            `json:"-"`
	OmitClaudeMd     bool              `json:"-"`
	// DefinitionMCPServers and DefinitionRequiredMCPServers are advisory for
	// the runtime prompt. The CLI registry still owns actual MCP connection
	// setup, but the child receives the same semantic constraints as the source
	// agent definition.
	DefinitionMCPServers         []string `json:"-"`
	DefinitionRequiredMCPServers []string `json:"-"`
	DefinitionSkills             []string `json:"-"`
	// DrainPendingMessages is provided for runtime background agents so the
	// child runner can inject SendMessage follow-ups into its next model turn.
	DrainPendingMessages func(context.Context) []string `json:"-"`
}

type Runner func(ctx context.Context, request Request) (string, error)

type Tool struct {
	defaultWorkDir string
	run            Runner
	background     *BackgroundManager
	taskManager    *coretasks.TaskManager
	forkEnabled    func() bool
	mcpServers     []string
}

func NewTool(defaultWorkDir string, run Runner) *Tool {
	return &Tool{
		defaultWorkDir: defaultWorkDir,
		run:            run,
		taskManager:    coretasks.DefaultManager(),
		forkEnabled:    ForkEnabledFromEnv,
	}
}

func NewToolWithBackgroundManager(defaultWorkDir string, run Runner, background *BackgroundManager) *Tool {
	tool := NewTool(defaultWorkDir, run)
	tool.background = background
	return tool
}

func NewToolWithTaskManager(defaultWorkDir string, run Runner, manager *coretasks.TaskManager) *Tool {
	tool := NewTool(defaultWorkDir, run)
	tool.taskManager = manager
	return tool
}

func (t *Tool) SetAvailableMCPServers(servers []string) {
	t.mcpServers = append([]string(nil), servers...)
}

func (t *Tool) Name() string {
	return "Agent"
}

func (t *Tool) Description() string {
	return "Run a bounded sub-agent prompt in an isolated engine invocation."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string"},"description":{"type":"string"},"subagent_type":{"type":"string"},"model":{"type":"string"},"cwd":{"type":"string"},"working_dir":{"type":"string"},"name":{"type":"string"},"team_name":{"type":"string"},"mode":{"type":"string"},"run_in_background":{"type":"boolean"},"isolation":{"type":"string","enum":["none","worktree","remote"]},"max_turns":{"type":"integer","minimum":1}},"required":["prompt"]}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelExecute
}

func (t *Tool) IsConcurrencySafe() bool {
	return false // agent spawns sub-engine which may modify state
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	if t.run == nil {
		return toolkit.Result{}, fmt.Errorf("agent runner is not configured")
	}

	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return toolkit.Result{}, err
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Description = strings.TrimSpace(req.Description)
	req.SubagentType = strings.TrimSpace(req.SubagentType)
	req.Model = strings.TrimSpace(req.Model)
	req.Cwd = strings.TrimSpace(req.Cwd)
	req.WorkingDir = strings.TrimSpace(req.WorkingDir)
	req.Name = strings.TrimSpace(req.Name)
	req.TeamName = strings.TrimSpace(req.TeamName)
	req.Mode = strings.TrimSpace(req.Mode)
	req.Isolation = strings.TrimSpace(strings.ToLower(req.Isolation))
	req.ParentAgentID = strings.TrimSpace(req.ParentAgentID)
	req.ParentSessionID = strings.TrimSpace(req.ParentSessionID)

	if req.Prompt == "" {
		return toolkit.Result{}, fmt.Errorf("prompt is required")
	}
	switch req.Isolation {
	case "", "none":
		req.Isolation = ""
	case "worktree":
	case "remote":
		return toolkit.Result{}, fmt.Errorf("agent isolation %q requires a remote agent backend", req.Isolation)
	default:
		return toolkit.Result{}, fmt.Errorf("agent isolation %q is invalid", req.Isolation)
	}
	if req.MaxTurns < 0 {
		return toolkit.Result{}, fmt.Errorf("max_turns must be positive")
	}
	if req.Cwd != "" && req.WorkingDir != "" && req.Cwd != req.WorkingDir {
		return toolkit.Result{}, fmt.Errorf("cwd and working_dir must match when both are provided")
	}
	if req.WorkingDir == "" {
		req.WorkingDir = req.Cwd
	}
	if req.WorkingDir == "" {
		req.WorkingDir = t.defaultWorkDir
	}
	if err := t.routeRequest(ctx, &req); err != nil {
		return toolkit.Result{}, err
	}
	if err := t.applyDefinitionDefaults(&req); err != nil {
		return toolkit.Result{}, err
	}
	switch req.Isolation {
	case "", "none":
		req.Isolation = ""
	case "worktree":
	case "remote":
		return toolkit.Result{}, fmt.Errorf("agent isolation %q requires a remote agent backend", req.Isolation)
	default:
		return toolkit.Result{}, fmt.Errorf("agent isolation %q is invalid", req.Isolation)
	}
	cleanup, err := t.prepareWorktreeIsolation(&req)
	if err != nil {
		return toolkit.Result{}, err
	}

	if req.RunInBackground {
		if t.background == nil {
			return t.startRuntimeLocalAgent(ctx, req, cleanup)
		}
		runner := t.run
		if cleanup != nil {
			runner = func(ctx context.Context, request Request) (string, error) {
				output, runErr := t.run(ctx, request)
				output = appendIsolationCleanupNote(output, cleanup())
				return output, runErr
			}
		}
		task, err := t.background.Start(ctx, req, runner)
		if err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return toolkit.Result{}, err
		}
		payload, _ := json.Marshal(map[string]any{
			"agent_id":        task.ID,
			"status":          task.Status,
			"description":     req.Description,
			"agent_type":      req.SubagentType,
			"worktree_path":   req.WorktreePath,
			"worktree_branch": req.WorktreeBranch,
		})
		return toolkit.Result{Output: string(payload)}, nil
	}

	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, err := t.run(ctx, req)
		if cleanup != nil {
			output = appendIsolationCleanupNote(output, cleanup())
		}
		done <- result{output: output, err: err}
	}()

	select {
	case <-ctx.Done():
		return toolkit.Result{}, ctx.Err()
	case outcome := <-done:
		if outcome.err != nil {
			return toolkit.Result{}, outcome.err
		}
		return toolkit.Result{Output: outcome.output}, nil
	}
}

func (t *Tool) routeRequest(ctx context.Context, req *Request) error {
	if agentCtx, ok := coreagent.AgentContextFrom(ctx); ok {
		if req.ParentAgentID == "" {
			req.ParentAgentID = agentCtx.AgentID
		}
		if req.ParentSessionID == "" {
			req.ParentSessionID = agentCtx.ParentSessionID
		}
		if req.ParentMetadata == nil {
			req.ParentMetadata = cloneStringMap(agentCtx.SessionMetadata)
		}
		if len(req.ParentMessages) == 0 {
			req.ParentMessages = append([]string(nil), agentCtx.RecentMessages...)
		}
	}
	if req.TeamName != "" || req.Name != "" {
		if req.TeamName == "" || req.Name == "" {
			return fmt.Errorf("team_name and name must be provided together")
		}
		if req.SubagentType == "" {
			req.SubagentType = req.Name
		}
		req.InvocationKind = string(coreagent.InvocationTeammate)
		req.RunInBackground = true
		return nil
	}

	if req.SubagentType != "" {
		req.InvocationKind = string(coreagent.InvocationSubagent)
		return nil
	}

	if t.isForkEnabled() {
		if agentCtx, ok := coreagent.AgentContextFrom(ctx); ok && agentCtx.InvocationKind == coreagent.InvocationFork {
			return fmt.Errorf("fork is not available inside a forked worker")
		}
		req.SubagentType = coreagent.FORK_SUBAGENT_TYPE
		req.InvocationKind = string(coreagent.InvocationFork)
		req.PermissionPolicy = string(coreagent.PermissionBubble)
		req.RunInBackground = true
		req.Prompt = prependForkContext(req.Prompt, req.ParentSessionID, req.ParentMessages)
		if req.Description == "" {
			req.Description = "Forked subagent"
		}
		return nil
	}

	req.SubagentType = "general-purpose"
	req.InvocationKind = string(coreagent.InvocationSubagent)
	return nil
}

func prependForkContext(prompt string, parentSessionID string, messages []string) string {
	if len(messages) == 0 && parentSessionID == "" {
		return prompt
	}
	var b strings.Builder
	b.WriteString("<fork-parent-context>\n")
	if parentSessionID != "" {
		b.WriteString("Parent session ID: " + parentSessionID + "\n")
	}
	if len(messages) > 0 {
		b.WriteString("Recent parent conversation:\n")
		for _, message := range messages {
			message = strings.TrimSpace(message)
			if message != "" {
				b.WriteString("- " + message + "\n")
			}
		}
	}
	b.WriteString("</fork-parent-context>\n\n")
	b.WriteString(prompt)
	return b.String()
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (t *Tool) applyDefinitionDefaults(req *Request) error {
	if req == nil || req.SubagentType == "" {
		return nil
	}
	result, err := coreagent.GetAgentDefinitionsWithOverrides(req.WorkingDir, false)
	if err != nil {
		return err
	}
	var def *coreagent.AgentDefinition
	for _, candidate := range result.Agents {
		if candidate != nil && string(candidate.AgentType) == req.SubagentType {
			def = candidate
			break
		}
	}
	if def == nil {
		return nil
	}
	req.DefinitionSource = string(def.Source)
	req.DefinitionMemory = strings.TrimSpace(def.Memory)
	req.PermissionPolicy = string(def.Permission)
	req.OmitClaudeMd = def.OmitClaudeMd
	req.DefinitionMCPServers = append([]string(nil), def.MCPServers...)
	req.DefinitionRequiredMCPServers = append([]string(nil), def.RequiredMCPServers...)
	req.DefinitionSkills = append([]string(nil), def.Skills...)
	if !coreagent.HasRequiredMCPServers(def, t.mcpServers) {
		return fmt.Errorf("agent %q requires MCP servers %s, but available MCP servers are %s", req.SubagentType, formatList(def.RequiredMCPServers), formatList(t.mcpServers))
	}
	if def.Background {
		req.RunInBackground = true
	}
	if req.Isolation == "" && strings.TrimSpace(def.Isolation) != "" {
		req.Isolation = strings.TrimSpace(strings.ToLower(def.Isolation))
	}
	if req.MaxTurns == 0 && def.MaxTurns > 0 {
		req.MaxTurns = def.MaxTurns
	}
	if req.Model == "" && def.Model != "" && def.Model != coreagent.ModelInherit {
		req.Model = string(def.Model)
	}
	return nil
}

func formatList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[" + strings.Join(values, ", ") + "]"
}

func (t *Tool) isForkEnabled() bool {
	if t.forkEnabled == nil {
		return false
	}
	return t.forkEnabled()
}

func (t *Tool) startRuntimeLocalAgent(ctx context.Context, req Request, cleanup isolationCleanupFunc) (toolkit.Result, error) {
	if t.taskManager == nil {
		return toolkit.Result{}, fmt.Errorf("local agent task manager is not configured")
	}
	if req.InvocationKind == string(coreagent.InvocationTeammate) {
		runner := func(ctx context.Context, runReq coretasks.InProcessTeammateRunRequest) (string, error) {
			childReq := req
			childReq.AgentID = runReq.TeammateID
			childReq.DrainPendingMessages = func(context.Context) []string {
				return drainPendingStrings(t.taskManager.DrainInProcessTeammateMessages(runReq.TaskID))
			}
			output, runErr := t.run(ctx, childReq)
			if cleanup != nil {
				output = appendIsolationCleanupNote(output, cleanup())
			}
			return output, runErr
		}
		task, err := t.taskManager.StartInProcessTeammate(ctx, coretasks.StartInProcessTeammateOptions{
			Prompt:          req.Prompt,
			Description:     req.Description,
			Name:            req.Name,
			TeamName:        req.TeamName,
			ParentAgentID:   req.ParentAgentID,
			ParentSessionID: req.ParentSessionID,
			AgentType:       req.SubagentType,
			Model:           req.Model,
			WorkingDir:      req.WorkingDir,
			WorktreePath:    req.WorktreePath,
			WorktreeBranch:  req.WorktreeBranch,
			IsBackgrounded:  true,
			Runner:          runner,
		})
		if err != nil {
			if cleanup != nil {
				_ = cleanup()
			}
			return toolkit.Result{}, err
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id":         task.ID,
			"agent_id":        task.TeammateID,
			"status":          task.Status,
			"description":     task.Description,
			"agent_type":      task.AgentType,
			"team_name":       task.TeamName,
			"name":            task.Name,
			"invocation_kind": req.InvocationKind,
			"worktree_path":   req.WorktreePath,
			"worktree_branch": req.WorktreeBranch,
		})
		return toolkit.Result{Output: string(payload)}, nil
	}
	runner := func(ctx context.Context, runReq coretasks.LocalAgentRunRequest) (string, error) {
		childReq := req
		childReq.AgentID = runReq.AgentID
		childReq.DrainPendingMessages = func(context.Context) []string {
			return drainPendingStrings(t.taskManager.DrainLocalAgentMessages(runReq.TaskID))
		}
		output, runErr := t.run(ctx, childReq)
		if cleanup != nil {
			output = appendIsolationCleanupNote(output, cleanup())
		}
		return output, runErr
	}
	task, err := t.taskManager.StartLocalAgent(ctx, coretasks.StartLocalAgentOptions{
		Prompt:          req.Prompt,
		Description:     req.Description,
		ParentAgentID:   req.ParentAgentID,
		ParentSessionID: req.ParentSessionID,
		AgentType:       req.SubagentType,
		Model:           req.Model,
		WorkingDir:      req.WorkingDir,
		WorktreePath:    req.WorktreePath,
		WorktreeBranch:  req.WorktreeBranch,
		IsBackgrounded:  true,
		Retain:          true,
		Runner:          runner,
	})
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return toolkit.Result{}, err
	}
	payload, _ := json.Marshal(map[string]any{
		"task_id":         task.ID,
		"agent_id":        task.AgentID,
		"status":          task.Status,
		"description":     task.Description,
		"agent_type":      task.AgentType,
		"invocation_kind": req.InvocationKind,
		"worktree_path":   req.WorktreePath,
		"worktree_branch": req.WorktreeBranch,
	})
	return toolkit.Result{Output: string(payload)}, nil
}

func ForkEnabledFromEnv() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDE_CODE_FORK_SUBAGENT")))
	return value == "1" || value == "true" || value == "yes"
}

type isolationCleanupFunc func() string

func (t *Tool) prepareWorktreeIsolation(req *Request) (isolationCleanupFunc, error) {
	if req.Isolation != "worktree" {
		return nil, nil
	}
	parentDir := req.WorkingDir
	branch := worktreeBranchName(req)
	manager := coordinator.NewWorktreeManager(parentDir)
	worktreePath, err := manager.Enter(branch)
	if err != nil {
		return nil, err
	}
	req.ParentWorkDir = parentDir
	req.WorktreePath = worktreePath
	req.WorktreeBranch = branch
	req.WorkingDir = worktreePath
	req.Cwd = worktreePath
	req.Prompt = coreagent.BuildWorktreeNotice(parentDir, worktreePath) + "\n\n" + req.Prompt

	return func() string {
		result, err := manager.Exit(coordinator.ExitWorktreeOptions{Action: "remove"})
		if err == nil {
			return result.Message
		}
		keep, keepErr := manager.Exit(coordinator.ExitWorktreeOptions{Action: "keep"})
		if keepErr == nil {
			return fmt.Sprintf("Kept worktree %s because automatic cleanup could not remove it safely: %v", keep.WorktreePath, err)
		}
		return fmt.Sprintf("Worktree cleanup failed for %s: %v; keep fallback also failed: %v", worktreePath, err, keepErr)
	}, nil
}

func worktreeBranchName(req *Request) string {
	kind := req.InvocationKind
	if kind == "" {
		kind = "agent"
	}
	name := req.SubagentType
	if req.Name != "" {
		name = req.Name
	}
	name = sanitizeBranchPart(name)
	if name == "" {
		name = "worker"
	}
	return fmt.Sprintf("claude-%s-%s-%d", sanitizeBranchPart(kind), name, time.Now().UnixNano())
}

func sanitizeBranchPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func appendIsolationCleanupNote(output string, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return output
	}
	if strings.TrimSpace(output) == "" {
		return "Worktree: " + note
	}
	return strings.TrimRight(output, "\n") + "\n\nWorktree: " + note
}

func drainPendingStrings(messages []interface{}, err error) []string {
	if err != nil || len(messages) == 0 {
		return nil
	}
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		text := strings.TrimSpace(fmt.Sprint(message))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

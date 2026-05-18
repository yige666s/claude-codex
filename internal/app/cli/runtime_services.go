package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/app/config"
	"claude-codex/internal/backend/services/agentsummary"
	"claude-codex/internal/backend/services/autodream"
	"claude-codex/internal/backend/services/magicdocs"
	coreagent "claude-codex/internal/harness/agent"
	"claude-codex/internal/harness/memdir"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/telemetry"
	toolkit "claude-codex/internal/harness/tools"
	agenttool "claude-codex/internal/harness/tools/agent"
	filetool "claude-codex/internal/harness/tools/file"
	"claude-codex/internal/public/fsutil"
)

var pathLikePattern = regexp.MustCompile("`([^`]+)`|([A-Za-z0-9_./-]+\\.[A-Za-z0-9_]+)")

type runtimeServices struct {
	workingDir string
	home       string

	autoDream *autodream.Service
	magicDocs *magicdocs.Service

	promptSuggestionCh chan string
	progressSink       func(toolkit.ProgressEvent)

	mu             sync.Mutex
	lastSuggestion string
}

func newRuntimeServices(
	workingDir string,
	home string,
	promptSuggestionCh chan string,
	progressSink func(toolkit.ProgressEvent),
) *runtimeServices {
	return &runtimeServices{
		workingDir:         workingDir,
		home:               home,
		autoDream:          autodream.NewService(autodream.DefaultConfig()),
		magicDocs:          magicdocs.NewService(),
		promptSuggestionCh: promptSuggestionCh,
		progressSink:       progressSink,
	}
}

func (r *runtimeServices) registerFileReadListener() func() {
	return filetool.RegisterReadListener(func(path string, content string) {
		if !r.magicDocs.Register(path, content) {
			if suggestion := r.suggestionForPath(path); suggestion != "" {
				r.emitSuggestion(suggestion)
			}
			return
		}
		if suggestion := r.suggestionForPath(path); suggestion != "" {
			r.emitSuggestion(suggestion)
		}
	})
}

func (r *runtimeServices) warmupMagicDocs() {
	_ = filepath.WalkDir(r.workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gocache", ".gomodcache", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".mdx") && !strings.HasSuffix(name, ".txt") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 256*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		r.magicDocs.Register(path, string(data))
		return nil
	})
}

func (r *runtimeServices) updatePromptSuggestion(session *state.Session) {
	if session == nil {
		return
	}
	for _, path := range extractPromptPaths(session.LastUserMessage()) {
		if suggestion := r.suggestionForPath(path); suggestion != "" {
			r.emitSuggestion(suggestion)
			return
		}
	}
}

func (r *runtimeServices) maybeRunAutoDream() {
	sessions, err := listSavedSessions(r.home, 12)
	if err != nil || len(sessions) == 0 {
		return
	}

	dreamPath := memdir.GetAutoMemDailyLogPath(r.workingDir, time.Now().Format("2006-01-02"))
	var lastConsolidatedAt time.Time
	if info, err := os.Stat(dreamPath); err == nil {
		lastConsolidatedAt = info.ModTime()
	}
	if !r.autoDream.ShouldRun(lastConsolidatedAt, sessions) {
		return
	}

	baseDir := filepath.Dir(dreamPath)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return
	}
	acquired, err := autodream.TryAcquireLock(baseDir)
	if err != nil || !acquired {
		return
	}
	defer func() {
		_ = autodream.ReleaseLock(baseDir)
	}()

	if err := fsutil.WriteFileAtomic(dreamPath, []byte(buildAutoDreamSummary(sessions)), 0o644); err != nil {
		return
	}

	r.emitSuggestion("AutoDream consolidated recent sessions into " + dreamPath)
}

func (r *runtimeServices) suggestionForPath(path string) string {
	tracked := r.magicDocs.Tracked()
	if len(tracked) == 0 {
		return ""
	}

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(r.workingDir, path)
	}

	type candidate struct {
		info  magicdocs.Info
		score int
	}
	var best candidate
	for _, info := range tracked {
		score := magicDocMatchScore(info, absPath)
		if score > best.score {
			best = candidate{info: info, score: score}
		}
	}
	if best.score == 0 {
		return ""
	}

	label := filepath.Base(best.info.Path)
	if best.info.Instructions != "" {
		return fmt.Sprintf("Consult %s (%s): %s", label, best.info.Title, best.info.Instructions)
	}
	return fmt.Sprintf("Consult %s before editing nearby files: %s", label, best.info.Title)
}

func (r *runtimeServices) emitSuggestion(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	r.mu.Lock()
	if text == r.lastSuggestion {
		r.mu.Unlock()
		return
	}
	r.lastSuggestion = text
	r.mu.Unlock()

	if r.promptSuggestionCh != nil {
		select {
		case r.promptSuggestionCh <- text:
		default:
		}
	}
}

func makeSubagentRunner(
	cfg config.Config,
	mode permissions.Mode,
	streams IO,
	skillManager *skills.SkillManager,
	requestHandler permissions.RequestHandler,
	progressSink func(toolkit.ProgressEvent),
	tracer telemetry.SessionTracer,
) agenttool.Runner {
	return func(ctx context.Context, request agenttool.Request) (string, error) {
		targetDir, cleanup, err := prepareSubagentWorkingDir(ctx, request)
		if err != nil {
			return "", err
		}
		defer cleanup()

		childCfg := cfg
		if request.Model != "" {
			childCfg.Model = request.Model
		}
		if request.MaxTurns > 0 {
			childCfg.MaxTurns = request.MaxTurns
		}

		options := subagentPermissionOptions(request.PermissionPolicy)
		if requestHandler != nil {
			options = append(options, permissions.WithRequestHandler(requestHandler))
		}
		childMode := subagentPermissionMode(mode, request.PermissionPolicy)

		childEngine, err := newEngineWithOptions(
			childCfg,
			childMode,
			targetDir,
			streams,
			nil,
			tracer,
			skillManager,
			options...,
		)
		if err != nil {
			return "", err
		}
		if request.DrainPendingMessages != nil {
			childEngine.SetPendingMessageProvider(request.DrainPendingMessages)
		}

		var summaryState sync.RWMutex
		currentSummary := agentInitialSummary(request)
		setSummary := func(value string) {
			value = strings.TrimSpace(value)
			if value == "" {
				return
			}
			summaryState.Lock()
			currentSummary = value
			summaryState.Unlock()
		}
		getSummary := func() string {
			summaryState.RLock()
			defer summaryState.RUnlock()
			return currentSummary
		}

		if progressSink != nil {
			progressSink(toolkit.ProgressEvent{ToolName: "Agent", Status: "started", Message: currentSummary})
		}

		childEngine.SetProgressCallback(func(event toolkit.ProgressEvent) {
			setSummary(agentProgressSummary(event))
			if progressSink != nil {
				progressSink(event)
			}
		})

		var stopSummary func()
		if progressSink != nil {
			stopSummary = agentsummary.NewService(750*time.Millisecond).Start(
				func(previous string) (string, error) {
					return getSummary(), nil
				},
				func(summary string) {
					progressSink(toolkit.ProgressEvent{
						ToolName: "Agent",
						Status:   "progress",
						Message:  summary,
					})
				},
			)
			defer stopSummary()
		}

		childSession := state.NewSession(targetDir)
		if request.ParentSessionID != "" {
			childSession.ParentID = request.ParentSessionID
		}
		if request.AgentID != "" {
			childSession.AgentID = request.AgentID
		}
		if childSession.Metadata == nil {
			childSession.Metadata = map[string]string{}
		}
		for key, value := range request.ParentMetadata {
			childSession.Metadata["parent_"+key] = value
		}
		childSession.Metadata["agent_invocation_kind"] = request.InvocationKind
		childSession.Metadata["agent_type"] = request.SubagentType
		childSession.Metadata["parent_agent_id"] = request.ParentAgentID
		childSession.Metadata["parent_session_id"] = request.ParentSessionID
		runCtx := coreagent.WithAgentContext(ctx, coreagent.AgentContext{
			AgentID:         request.AgentID,
			ParentSessionID: childSession.ID,
			AgentType:       request.SubagentType,
			SubagentName:    request.Name,
			TeamName:        request.TeamName,
			InvocationKind:  coreagent.AgentInvocationKind(request.InvocationKind),
			InvocationID:    request.InvocationKind + ":" + request.AgentID,
		})
		result, err := childEngine.Run(runCtx, childSession, buildSubagentPrompt(request))
		if err != nil {
			if progressSink != nil {
				progressSink(toolkit.ProgressEvent{ToolName: "Agent", Status: "failed", Message: getSummary()})
			}
			return "", err
		}

		finalSummary := summarizeAgentOutput(result.Output)
		setSummary(finalSummary)
		if progressSink != nil {
			progressSink(toolkit.ProgressEvent{
				ToolName: "Agent",
				Status:   "completed",
				Progress: 1,
				Message:  getSummary(),
			})
		}
		return result.Output, nil
	}
}

func subagentPermissionMode(parent permissions.Mode, policy string) permissions.Mode {
	switch coreagent.PermissionMode(strings.TrimSpace(strings.ToLower(policy))) {
	case coreagent.PermissionAllow:
		return permissions.ModeBypass
	case coreagent.PermissionDeny:
		return permissions.ModeDefault
	default:
		return parent
	}
}

func subagentPermissionOptions(policy string) []permissions.Option {
	switch coreagent.PermissionMode(strings.TrimSpace(strings.ToLower(policy))) {
	case coreagent.PermissionDeny:
		return []permissions.Option{
			permissions.WithDecisionResolver(permissions.DecisionResolverFunc(func(_ context.Context, request permissions.Request) (permissions.Decision, bool, error) {
				return permissions.Decision{
					Behavior: permissions.BehaviorDeny,
					Reason:   "Subagent permission policy denies " + request.ToolName + ".",
				}, true, nil
			})),
		}
	default:
		return nil
	}
}

func prepareSubagentWorkingDir(ctx context.Context, request agenttool.Request) (string, func(), error) {
	targetDir := request.WorkingDir
	if targetDir == "" {
		targetDir = request.Cwd
	}
	if targetDir == "" {
		targetDir, _ = os.Getwd()
	}
	cleanup := func() {}
	if request.Isolation != "worktree" || request.WorktreePath != "" {
		return targetDir, cleanup, nil
	}

	rootOutput, err := exec.CommandContext(ctx, "git", "-C", targetDir, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return "", cleanup, fmt.Errorf("agent worktree isolation requires a git repository: %s", strings.TrimSpace(string(rootOutput)))
	}
	root := strings.TrimSpace(string(rootOutput))
	parent := filepath.Join(os.TempDir(), "claude-codex-agent-worktrees")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", cleanup, err
	}
	worktreeDir, err := os.MkdirTemp(parent, "agent-*")
	if err != nil {
		return "", cleanup, err
	}
	_ = os.Remove(worktreeDir)

	output, err := exec.CommandContext(ctx, "git", "-C", root, "worktree", "add", "--detach", worktreeDir, "HEAD").CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(worktreeDir)
		return "", cleanup, fmt.Errorf("create agent worktree: %s", strings.TrimSpace(string(output)))
	}
	cleanup = func() {
		_ = exec.Command("git", "-C", root, "worktree", "remove", "--force", worktreeDir).Run()
		_ = os.RemoveAll(worktreeDir)
	}
	return worktreeDir, cleanup, nil
}

func agentInitialSummary(request agenttool.Request) string {
	if request.Description != "" {
		return request.Description
	}
	if request.SubagentType != "" {
		return "Running " + request.SubagentType + " subagent"
	}
	return summarizeAgentOutput(request.Prompt)
}

func agentProgressSummary(event toolkit.ProgressEvent) string {
	switch event.Status {
	case "started":
		if event.Message != "" {
			return event.Message
		}
		return "Subagent started"
	case "completed":
		if event.Message != "" {
			return event.Message
		}
		return "Subagent completed"
	case "failed":
		if event.Message != "" {
			return "Subagent failed: " + event.Message
		}
		return "Subagent failed"
	default:
		if event.Message != "" {
			return event.Message
		}
		if event.ToolName != "" {
			return "Using " + event.ToolName
		}
		return "Subagent running"
	}
}

func summarizeAgentOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "Subagent completed"
	}
	output = strings.ReplaceAll(output, "\n", " ")
	if len(output) > 120 {
		return output[:117] + "..."
	}
	return output
}

func magicDocMatchScore(info magicdocs.Info, path string) int {
	infoPath := filepath.Clean(info.Path)
	targetPath := filepath.Clean(path)
	switch {
	case infoPath == targetPath:
		return 100
	case filepath.Dir(infoPath) == filepath.Dir(targetPath):
		return 80
	case strings.HasPrefix(targetPath, filepath.Dir(infoPath)+string(filepath.Separator)):
		return 60
	case strings.Contains(strings.ToLower(targetPath), strings.ToLower(info.Title)):
		return 40
	default:
		return 0
	}
}

func extractPromptPaths(prompt string) []string {
	matches := pathLikePattern.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := strings.TrimSpace(match[1])
		if candidate == "" {
			candidate = strings.TrimSpace(match[2])
		}
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		paths = append(paths, candidate)
	}
	return paths
}

func listSavedSessions(home string, limit int) ([]*state.Session, error) {
	dir := filepath.Join(home, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	type candidate struct {
		path string
		mod  time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path: filepath.Join(dir, entry.Name()),
			mod:  info.ModTime(),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].mod.After(candidates[j].mod)
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	sessions := make([]*state.Session, 0, len(candidates))
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate.path)
		if err != nil {
			continue
		}
		var session state.Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		sessions = append(sessions, &session)
	}
	return sessions, nil
}

func buildAutoDreamSummary(sessions []*state.Session) string {
	now := time.Now().UTC()
	sessionIDs := make([]string, 0, len(sessions))
	lines := []string{
		"---",
		"name: Auto Dream " + now.Format("2006-01-02"),
		"description: Consolidated recent session activity",
		"type: reference",
		"---",
		"",
		"# Auto Dream",
		"",
		"Generated at " + now.Format(time.RFC3339),
		"",
		"## Sessions",
	}
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.ID)
		lines = append(lines,
			"",
			fmt.Sprintf("- `%s`", session.ID),
			"  - user: "+summarizeAgentOutput(session.LastUserMessage()),
			"  - assistant: "+summarizeAgentOutput(lastAssistantMessage(session)),
		)
	}
	lines = append(lines,
		"",
		"## Consolidation Prompt",
		"",
		autodream.NewService(autodream.DefaultConfig()).Prompt(sessionIDs),
	)
	return strings.Join(lines, "\n")
}

func lastAssistantMessage(session *state.Session) string {
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role == "assistant" {
			return session.Messages[i].Content
		}
	}
	return ""
}

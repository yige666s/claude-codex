package run

import (
	"context"
	"os"
	"strings"
	"time"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/skills"
	coretasks "claude-codex/internal/harness/tasks"
	"claude-codex/internal/harness/tools"
	agenttool "claude-codex/internal/harness/tools/agent"
	bashtool "claude-codex/internal/harness/tools/bash"
	filetool "claude-codex/internal/harness/tools/file"
	searchtool "claude-codex/internal/harness/tools/search"
	sendmessagetool "claude-codex/internal/harness/tools/sendmessage"
	skilltool "claude-codex/internal/harness/tools/skill"
	tasktool "claude-codex/internal/harness/tools/tasks"
	teamtool "claude-codex/internal/harness/tools/team"
	webtool "claude-codex/internal/harness/tools/web"
)

type registryCollaborationDeps struct {
	coordinatorManager *coordinator.Manager
	taskManager        *coretasks.TaskManager
	runSubagent        agenttool.Runner
}

func buildRegistry(root string, skillManager *skills.SkillManager, allowDangerous bool, artifactWriter agentruntime.ArtifactWriter, artifactMaxBytes int64, networkAllowlist []string, allowedTools []string, sandboxBash *agentruntime.SandboxBashTool, collaboration registryCollaborationDeps) *tools.Registry {
	allowed := toolNameSet(allowedTools)
	enabled := func(name string) bool {
		return len(allowed) == 0 || allowed[name]
	}
	taskManager := collaboration.taskManager
	if taskManager == nil {
		taskManager = coretasks.DefaultManager()
	}
	toolList := make([]tools.Tool, 0, 18)
	if enabled("Read") {
		toolList = append(toolList, filetool.NewReadTool(root))
	}
	if enabled("Glob") {
		toolList = append(toolList, searchtool.NewGlobTool(root))
	}
	if enabled("Grep") {
		toolList = append(toolList, searchtool.NewGrepTool(root))
	}
	if enabled("WebSearch") {
		toolList = append(toolList, webtool.NewSearchToolWithAllowlist(nil, networkAllowlist))
	}
	if enabled("WebFetch") {
		toolList = append(toolList, webtool.NewFetchToolWithAllowlist(nil, networkAllowlist))
	}
	if enabled("Skill") {
		toolList = append(toolList, skilltool.NewToolWithOptions(skillManager, skilltool.Options{
			DefaultDir: root,
		}))
	}
	if artifactWriter != nil && enabled(agentruntime.ArtifactToolName) {
		toolList = append(toolList, agentruntime.NewArtifactToolWithLimit(artifactWriter, root, artifactMaxBytes))
	}
	if collaboration.runSubagent != nil && enabled("Agent") {
		agentTool := agenttool.NewToolWithTaskManager(root, collaboration.runSubagent, taskManager)
		toolList = append(toolList, agentTool)
	}
	if enabled("TaskCreate") {
		toolList = append(toolList, tasktool.NewTaskCreateTool())
	}
	if enabled("TaskGet") {
		toolList = append(toolList, tasktool.NewTaskGetToolWithManager(taskManager))
	}
	if enabled("TaskList") {
		toolList = append(toolList, tasktool.NewTaskListToolWithManager(taskManager))
	}
	if enabled("TaskUpdate") {
		toolList = append(toolList, tasktool.NewTaskUpdateTool())
	}
	if enabled("TaskStop") {
		toolList = append(toolList, tasktool.NewTaskStopToolWithManager(taskManager))
	}
	if enabled("TaskOutput") {
		toolList = append(toolList, tasktool.NewTaskOutputToolWithManager(taskManager))
	}
	if collaboration.coordinatorManager != nil && enabled("TeamCreate") {
		toolList = append(toolList, teamtool.NewTeamCreateTool(collaboration.coordinatorManager))
	}
	if collaboration.coordinatorManager != nil && enabled("TeamDelete") {
		toolList = append(toolList, teamtool.NewTeamDeleteTool(collaboration.coordinatorManager))
	}
	if enabled("SendMessage") {
		toolList = append(toolList, &sendmessagetool.Tool{TaskManager: taskManager})
	}
	if sandboxBash != nil && enabled("Bash") {
		toolList = append(toolList, sandboxBash)
	} else if allowDangerous {
		if enabled("Write") {
			toolList = append(toolList, filetool.NewWriteTool(root))
		}
		if enabled("Edit") {
			toolList = append(toolList, filetool.NewEditTool(root))
		}
		if enabled("Bash") {
			toolList = append(toolList, bashtool.NewTool(root))
		}
	}
	return tools.NewRegistry(toolList...)
}

func allowedToolNames(allowDangerous bool) []string {
	names := []string{
		"Read", "Glob", "Grep", "WebSearch", "WebFetch", "Skill", agentruntime.ArtifactToolName,
		"Agent", "TaskCreate", "TaskGet", "TaskList", "TaskUpdate", "TaskStop", "TaskOutput",
		"TeamCreate", "TeamDelete", "SendMessage", "Bash",
	}
	if allowDangerous {
		names = append(names, "Write", "Edit")
	}
	return names
}

func consumerChatToolNames() []string {
	return []string{
		"WebSearch", "WebFetch", "Skill", agentruntime.ArtifactToolName,
		"Agent", "TaskCreate", "TaskGet", "TaskList", "TaskUpdate", "TaskStop", "TaskOutput",
		"TeamCreate", "TeamDelete", "SendMessage",
	}
}

func collaborationSafeWriteToolNames() []string {
	return []string{"TaskCreate", "TaskUpdate", "TaskStop", "TeamCreate", "TeamDelete", "SendMessage"}
}

func collaborationSafeExecuteToolNames() []string {
	return []string{"Agent"}
}

func effectiveAllowedToolNames(global []string, scope agentruntime.Scope) []string {
	if scope.SkillScoped {
		if len(cleanCSVValues(scope.AllowedTools)) == 0 {
			return []string{"__no_tools_allowed__"}
		}
		return scopedAllowedTools(global, scope.AllowedTools)
	}
	if scope.InternalToolScope {
		if len(cleanCSVValues(scope.AllowedTools)) == 0 {
			return []string{"__no_tools_allowed__"}
		}
		return scopedAllowedTools(global, scope.AllowedTools)
	}
	consumerTools := consumerChatToolNames()
	for _, name := range global {
		if strings.HasPrefix(name, "mcp__") {
			consumerTools = append(consumerTools, name)
		}
	}
	consumerAllowed := scopedAllowedTools(global, consumerTools)
	if len(cleanCSVValues(scope.AllowedTools)) > 0 {
		return scopedAllowedTools(consumerAllowed, scope.AllowedTools)
	}
	return consumerAllowed
}

func toolNameSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func scopedAllowedTools(global, scoped []string) []string {
	if len(scoped) == 0 {
		return global
	}
	globalSet := make(map[string]bool, len(global))
	for _, name := range global {
		globalSet[name] = true
	}
	out := make([]string, 0, len(scoped))
	for _, name := range scoped {
		toolName := scopedToolName(name)
		if globalSet[toolName] {
			out = append(out, toolName)
		}
	}
	if len(out) == 0 {
		return []string{"__no_tools_allowed__"}
	}
	return out
}

func scopedToolName(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "("); idx > 0 && strings.HasSuffix(value, ")") {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

func buildSandboxBashRuntime(config agentruntime.SkillShellSandboxConfig, root string, scope agentruntime.Scope) *agentruntime.SandboxBashTool {
	if scope.SkillShellSandbox.Runner != "" {
		config = scope.SkillShellSandbox
	}
	if (!scope.SkillScoped && !scope.InternalToolScope) || !allowsTool(scope.AllowedTools, "Bash") {
		return nil
	}
	shell := scope.SkillShell
	if shell == "" {
		shell = skills.ShellBash
	}
	if config.DockerEnabled() {
		runtime := agentruntime.NewDockerSkillShellRuntime(
			config,
			shell,
			root,
			startupconfig.FirstNonEmpty(scope.SkillRoot, root),
			scope.SkillShellEnv,
			scope.AllowedTools,
		)
		return agentruntime.NewSandboxBashTool(runtime)
	}
	if strings.EqualFold(strings.TrimSpace(config.Runner), "local") || strings.TrimSpace(config.Runner) == "" {
		runtime := agentruntime.NewLocalSkillShellRuntime(
			config,
			shell,
			root,
			startupconfig.FirstNonEmpty(scope.SkillRoot, root),
			scope.SkillShellEnv,
			scope.NetworkAllowlist,
			scope.AllowedTools,
		)
		return agentruntime.NewSandboxBashTool(runtime)
	}
	return nil
}

func warmSkillSandboxImages(ctx context.Context, images []string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, result := range agentruntime.WarmDockerSkillSandboxImages(ctx, images) {
		switch {
		case result.Error != nil:
			logInfof("skill sandbox image warm failed: image=%s pulled=%t duration=%s error=%v", result.Image, result.Pulled, result.Duration.Round(time.Millisecond), result.Error)
		case result.Pulled:
			logInfof("skill sandbox image pre-pulled: image=%s duration=%s", result.Image, result.Duration.Round(time.Millisecond))
		default:
			logInfof("skill sandbox image already local: image=%s check_duration=%s", result.Image, result.Duration.Round(time.Millisecond))
		}
	}
}

func startSkillSandboxWarmPool(ctx context.Context, config agentruntime.SkillShellSandboxConfig, images []string, size int) {
	if size <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	pool, err := agentruntime.StartDockerSkillWarmPool(ctx, config, images, size)
	if err != nil {
		logInfof("skill sandbox warm pool disabled: %v", err)
		return
	}
	if pool == nil {
		logInfof("skill sandbox warm pool skipped: runner=%s size=%d", config.Runner, size)
		return
	}
	agentruntime.SetDefaultDockerSkillWarmPool(pool)
	logInfof("skill sandbox warm pool started: size=%d images=%s", size, strings.Join(append([]string{config.Image}, images...), ","))
}

func allowsTool(values []string, toolName string) bool {
	for _, value := range values {
		if strings.EqualFold(scopedToolName(value), toolName) {
			return true
		}
	}
	return false
}

func scopedNetworkAllowlist(global, scoped []string) []string {
	scoped = cleanCSVValues(scoped)
	if len(scoped) == 0 {
		return global
	}
	global = cleanCSVValues(global)
	if len(global) == 0 {
		return scoped
	}
	globalSet := make(map[string]bool, len(global))
	for _, name := range global {
		globalSet[strings.ToLower(name)] = true
	}
	out := make([]string, 0, len(scoped))
	for _, name := range scoped {
		if globalSet[strings.ToLower(name)] {
			out = append(out, name)
		}
	}
	return out
}

func cleanCSVValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
	}
	return out
}

func loadSkills(skillDirs []string) *skills.SkillManager {
	manager := skills.NewSkillManager()
	if err := manager.LoadBundledSkills(); err != nil {
		logInfof("warning: failed to load bundled skills: %v", err)
	}
	for _, dir := range skillDirs {
		dir = strings.TrimSpace(os.ExpandEnv(dir))
		if dir == "" {
			continue
		}
		if err := manager.LoadSkillsFromDirectory(dir, skills.SourceFile); err != nil {
			logInfof("warning: failed to load skills from %s: %v", dir, err)
		}
	}
	stats := manager.GetStats()
	logInfof("skills loaded: total=%d bundled=%d dynamic=%d user_invocable=%d", stats.TotalSkills, stats.BundledSkills, stats.DynamicSkills, stats.UserInvocable)
	return manager
}

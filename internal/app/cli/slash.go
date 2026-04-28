package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	appsettings "claude-codex/internal/app/settings"
	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/memory"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/plugins"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
)

type slashContext struct {
	cfg             *config.Config
	home            string
	defaultWorkDir  string
	streams         IO
	saveSession     bool
	newEngineForDir func(string) (*engine.Engine, error)
	skillManager    *skills.SkillManager
}

var commandRegistry *Registry

func init() {
	commandRegistry = NewRegistry()
	registerCommands()
}

func registerCommands() {
	commands := []*Command{
		{
			Name:        "/help",
			Aliases:     []string{"/h", "/?"},
			Description: "Show this help message",
			Handler:     wrapHandler(handleHelpCommand),
		},
		{
			Name:        "/history",
			Aliases:     []string{"/hist"},
			Usage:       "[<limit>]",
			Description: "Show recent command history",
			Handler:     wrapHandlerWithArgs(handleHistoryCommand),
		},
		{
			Name:        "/diff",
			Usage:       "[<path>]",
			Description: "Show git diff for current changes",
			Handler:     wrapHandlerWithArgs(handleDiffCommand),
		},
		{
			Name:        "/config",
			Usage:       "[show|path|set <key> <value>|settings <list|get|set>]",
			Description: "Show, get path, or set configuration/settings values",
			Handler:     wrapHandlerWithArgs(handleConfigCommand),
		},
		{
			Name:        "/theme",
			Usage:       "[light|dark]",
			Description: "Get or set UI theme",
			Handler:     wrapHandlerWithArgs(handleThemeCommand),
		},
		{
			Name:        "/model",
			Usage:       "[show|default|<model>]",
			Description: "Show or set the active model",
			Handler:     wrapHandlerWithArgs(handleModelCommand),
		},
		{
			Name:        "/mode",
			Usage:       "[show|default|plan|bypass|auto]",
			Description: "Show or set the permission mode",
			Handler:     wrapHandlerWithArgs(handleModeCommand),
		},
		{
			Name:        "/mcp",
			Usage:       "[list|add|remove]",
			Description: "Manage MCP servers",
			Handler:     wrapHandlerWithArgs(handleMCPCommand),
		},
		{
			Name:        "/doctor",
			Aliases:     []string{"/doc"},
			Description: "Check environment and dependencies",
			Handler:     wrapHandler(handleDoctorCommand),
		},
		{
			Name:        "/login",
			Usage:       "[status|console]",
			Description: "Authenticate with OAuth and persist bridge credentials",
			Handler:     wrapHandlerWithArgs(handleLoginCommand),
		},
		{
			Name:        "/logout",
			Description: "Remove persisted OAuth and trusted device credentials",
			Handler:     wrapHandlerWithArgs(handleLogoutCommand),
		},
		{
			Name:        "/cost",
			Usage:       "[<session-id>|latest]",
			Description: "Show token usage and estimated cost",
			Handler:     wrapHandlerWithArgs(handleCostCommand),
		},
		{
			Name:        "/memory",
			Aliases:     []string{"/mem"},
			Usage:       "[show|list|append|edit|delete|search|stats] [<file>] [args...]",
			Description: "Manage memory files",
			Handler:     wrapHandlerWithArgs(handleMemoryCommand),
		},
		{
			Name:        "/resume",
			Usage:       "[<session-id>|latest] [--from-turn <n>] [<prompt>]",
			Description: "Resume a previous session, optionally from a specific turn",
			Handler:     handleResumeCommandWrapped,
		},
		{
			Name:        "/commit",
			Description: "Create a git commit with AI-generated message",
			Handler:     wrapHandlerWithArgs(handleCommitCommand),
		},
		{
			Name:        "/review",
			Description: "Review code changes and run checks",
			Handler:     wrapHandlerWithArgs(handleReviewCommand),
		},
		{
			Name:        "/compact",
			Description: "Compact session history",
			Handler:     wrapHandlerWithArgs(handleCompactCommand),
		},
		{
			Name:        "/session",
			Usage:       "[tag|search|branch|export|import|archive|cleanup|stats]",
			Description: "Advanced session management",
			Handler:     wrapHandlerWithArgs(handleSessionCommand),
		},
		{
			Name:        "/limits",
			Aliases:     []string{"/quota"},
			Description: "Show current API rate limit status",
			Handler:     wrapHandlerWithArgs(handleLimits),
		},
		{
			Name:        "/mem2",
			Aliases:     []string{"/memory2"},
			Usage:       "[list|show|search|filter|index|stats] [args...]",
			Description: "Manage session memories (new system)",
			Handler:     wrapHandlerWithArgs(handleMemoryV2Command),
		},
		{
			Name:        "/status",
			Description: "Show current CLI, git, auth, skill, and plugin status",
			Handler:     wrapHandlerWithArgs(handleStatusCommand),
		},
		{
			Name:        "/files",
			Usage:       "[tracked|changed|all]",
			Description: "List project files",
			Handler:     wrapHandlerWithArgs(handleFilesCommand),
		},
		{
			Name:        "/permissions",
			Usage:       "[list|add <allow|deny|ask> <rule>|remove <allow|deny|ask> <rule>]",
			Description: "Show or update permission rules",
			Handler:     wrapHandlerWithArgs(handlePermissionsCommand),
		},
		{
			Name:        "/plugin",
			Usage:       "[list|install-local <id> <path>|uninstall <id>|enable <id>|disable <id>]",
			Description: "Manage plugins",
			Handler:     wrapHandlerWithArgs(handlePluginCommand),
		},
		{
			Name:        "/branch",
			Usage:       "[show|list|create <name>|switch <name>|delete <name>]",
			Description: "Manage git branches",
			Handler:     wrapHandlerWithArgs(handleBranchCommand),
		},
		{
			Name:        "/tag",
			Usage:       "[list|add <tag>|remove <tag>] [<session-id>|latest]",
			Description: "Manage session tags",
			Handler:     wrapHandlerWithArgs(handleTagCommand),
		},
		{
			Name:        "/export",
			Usage:       "[<session-id>|latest] [json|markdown] [<output-path>]",
			Description: "Export a saved session",
			Handler:     wrapHandlerWithArgs(handleExportCommand),
		},
		{
			Name:        "/usage",
			Usage:       "[summary|sessions] [<limit>]",
			Description: "Show local session token usage",
			Handler:     wrapHandlerWithArgs(handleUsageCommand),
		},
	}

	for _, cmd := range commands {
		if err := commandRegistry.Register(cmd); err != nil {
			panic(fmt.Sprintf("failed to register command %s: %v", cmd.Name, err))
		}
	}
}

// wrapHandler wraps a handler that takes no args
func wrapHandler(fn func(slashContext) error) CommandHandler {
	return func(ctx context.Context, args []string, slash slashContext) error {
		return fn(slash)
	}
}

// wrapHandlerWithArgs wraps a handler that takes args
func wrapHandlerWithArgs(fn func([]string, slashContext) error) CommandHandler {
	return func(ctx context.Context, args []string, slash slashContext) error {
		return fn(args, slash)
	}
}

// handleResumeCommandWrapped wraps the resume command which needs context
func handleResumeCommandWrapped(ctx context.Context, args []string, slash slashContext) error {
	return handleResumeCommand(ctx, args, slash)
}

func runSlashCommand(ctx context.Context, line string, slash slashContext) error {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return nil
	}

	output, err := executeSlashLikeCommand(ctx, fields[0], fields[1:], slash)
	if err != nil {
		if output != "" {
			_, _ = fmt.Fprint(slash.streams.Out, output)
		}
		return err
	}

	if strings.HasPrefix(output, "__SKILL_PROMPT__") {
		return executeGeneratedSkillPrompt(ctx, strings.TrimPrefix(output, "__SKILL_PROMPT__"), slash)
	}
	if output == "" {
		return nil
	}
	_, err = fmt.Fprint(slash.streams.Out, output)
	return err
}

func handleThemeCommand(args []string, slash slashContext) error {
	if len(args) == 0 {
		_, err := fmt.Fprintln(slash.streams.Out, slash.cfg.Theme)
		return err
	}

	theme := strings.ToLower(strings.TrimSpace(args[0]))
	switch theme {
	case "light", "dark":
	default:
		return fmt.Errorf("usage: /theme [light|dark]")
	}

	slash.cfg.Theme = theme
	if err := config.Save(*slash.cfg); err != nil {
		return err
	}

	_, err := fmt.Fprintf(slash.streams.Out, "theme set to %s\n", theme)
	return err
}

func handleModelCommand(args []string, slash slashContext) error {
	switch {
	case len(args) == 0, isModelInfoArg(args[0]):
		current := strings.TrimSpace(slash.cfg.Model)
		if current == "" {
			current = config.Default().Model
		}
		suffix := ""
		if current == config.Default().Model {
			suffix = " (default)"
		}
		_, err := fmt.Fprintf(slash.streams.Out, "current model: %s%s\n", current, suffix)
		return err
	case isModelHelpArg(args[0]):
		return fmt.Errorf("usage: /model [show|default|<model>]")
	case len(args) == 1:
		model := strings.TrimSpace(args[0])
		if model == "" {
			return fmt.Errorf("usage: /model [show|default|<model>]")
		}
		if strings.EqualFold(model, "default") {
			model = config.Default().Model
		}
		slash.cfg.Model = model
		if err := config.Save(*slash.cfg); err != nil {
			return err
		}
		label := "updated"
		if model == config.Default().Model {
			label = "reset"
		}
		_, err := fmt.Fprintf(slash.streams.Out, "%s model to %s\n", label, model)
		return err
	default:
		return fmt.Errorf("usage: /model [show|default|<model>]")
	}
}

func handleModeCommand(args []string, slash slashContext) error {
	switch {
	case len(args) == 0 || strings.EqualFold(strings.TrimSpace(args[0]), "show"):
		current := strings.TrimSpace(slash.cfg.PermissionMode)
		if current == "" {
			current = config.Default().PermissionMode
		}
		suffix := ""
		if current == config.Default().PermissionMode {
			suffix = " (default)"
		}
		_, err := fmt.Fprintf(slash.streams.Out, "current mode: %s%s\n", current, suffix)
		return err
	case len(args) == 1:
		mode := strings.TrimSpace(args[0])
		if mode == "" {
			return fmt.Errorf("usage: /mode [show|default|plan|bypass|auto]")
		}
		if strings.EqualFold(mode, "default") {
			mode = config.Default().PermissionMode
		}
		if _, err := permissions.ParseMode(mode); err != nil {
			return fmt.Errorf("usage: /mode [show|default|plan|bypass|auto]")
		}
		slash.cfg.PermissionMode = mode
		if err := config.Save(*slash.cfg); err != nil {
			return err
		}
		label := "updated"
		if mode == config.Default().PermissionMode {
			label = "reset"
		}
		_, err := fmt.Fprintf(slash.streams.Out, "%s mode to %s\n", label, mode)
		return err
	default:
		return fmt.Errorf("usage: /mode [show|default|plan|bypass|auto]")
	}
}

func handleConfigCommand(args []string, slash slashContext) error {
	switch {
	case len(args) == 0, args[0] == "show":
		data, err := json.MarshalIndent(slash.cfg, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(slash.streams.Out, string(data))
		return err
	case args[0] == "path":
		path, err := config.ConfigPath()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(slash.streams.Out, path)
		return err
	case args[0] == "settings":
		return handleConfigSettingsCommand(args[1:], slash)
	case len(args) >= 3 && args[0] == "set":
		if err := setConfigValue(slash.cfg, args[1], strings.Join(args[2:], " ")); err != nil {
			return err
		}
		if err := config.Save(*slash.cfg); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "updated %s\n", args[1])
		return err
	default:
		return fmt.Errorf("usage: /config [show|path|set <key> <value>|settings <list|get|set>]")
	}
}

func handleConfigSettingsCommand(args []string, slash slashContext) error {
	switch {
	case len(args) == 0 || args[0] == "list":
		settings := appsettings.ListSupportedSettings()
		lines := make([]string, 0, len(settings))
		for _, setting := range settings {
			suffix := ""
			if len(setting.Options) > 0 {
				suffix = " [" + strings.Join(setting.Options, "|") + "]"
			}
			lines = append(lines, fmt.Sprintf("%s\t%s%s\t%s", setting.Key, setting.Type, suffix, setting.Description))
		}
		_, err := fmt.Fprintln(slash.streams.Out, strings.Join(lines, "\n"))
		return err
	case len(args) == 2 && args[0] == "get":
		setting, ok := appsettings.GetSupportedSetting(args[1])
		if !ok {
			return fmt.Errorf("unsupported settings key %q", args[1])
		}
		merged := appsettings.LoadMergedSettings(slash.defaultWorkDir)
		if len(merged.Errors) > 0 {
			return fmt.Errorf("cannot read invalid settings: %s", merged.Errors[0].Message)
		}
		value, ok := appsettings.ReadSettingValue(merged.Settings, setting)
		if !ok {
			value = nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "%s = %s\n", setting.Key, string(data))
		return err
	case len(args) >= 3 && args[0] == "set":
		setting, ok := appsettings.GetSupportedSetting(args[1])
		if !ok {
			return fmt.Errorf("unsupported settings key %q", args[1])
		}
		value, err := appsettings.CoerceSupportedSettingValue(setting, strings.Join(args[2:], " "))
		if err != nil {
			return err
		}
		update := appsettings.BuildSettingUpdate(setting, value)
		if err := appsettings.UpdateSettingsForSource(appsettings.EditableUser, slash.defaultWorkDir, update); err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "updated settings %s\n", setting.Key)
		return err
	default:
		return fmt.Errorf("usage: /config settings [list|get <key>|set <key> <value>]")
	}
}

func handleDoctorCommand(slash slashContext) error {
	path, err := config.ConfigPath()
	if err != nil {
		return err
	}

	lines := []string{
		formatToolCheck("go"),
		formatToolCheck("git"),
		formatToolCheck("rg"),
		fmt.Sprintf("claude-codex-home: %s", slash.home),
		fmt.Sprintf("config-path: %s", path),
		fmt.Sprintf("default-workdir: %s", slash.defaultWorkDir),
		fmt.Sprintf("mcp-servers: %d", len(slash.cfg.MCPServers)),
	}
	_, err = fmt.Fprintln(slash.streams.Out, strings.Join(lines, "\n"))
	return err
}

func handleLoginCommand(args []string, slash slashContext) error {
	manager, err := appauth.NewManager(*slash.cfg, nil)
	if err != nil {
		return err
	}

	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		status, err := manager.Status(context.Background())
		if err != nil {
			return err
		}
		if !status.Authenticated {
			_, err = fmt.Fprintln(slash.streams.Out, "login status: not authenticated")
			return err
		}
		_, err = fmt.Fprintf(
			slash.streams.Out,
			"login status: authenticated\nexpires_at: %s\nscopes: %s\ntrusted_device: %t\n",
			status.ExpiresAt.Format(time.RFC3339),
			strings.Join(status.Scopes, ", "),
			status.HasTrustedDevice,
		)
		return err
	}

	loginWithClaudeAI := true
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "console") {
		loginWithClaudeAI = false
	}

	_, err = fmt.Fprintln(slash.streams.Out, "starting oauth login flow...")
	if err != nil {
		return err
	}

	tokens, err := manager.Login(context.Background(), func(manualURL, automaticURL string) error {
		_, err := fmt.Fprintf(
			slash.streams.Out,
			"Open the authorization URL in your browser:\n%s\n\nManual fallback URL:\n%s\n\nWaiting for OAuth callback on localhost...\n",
			automaticURL,
			manualURL,
		)
		return err
	}, appauth.LoginOptions{
		LoginWithClaudeAI: loginWithClaudeAI,
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		slash.streams.Out,
		"login successful\nexpires_at: %s\nscopes: %s\n",
		time.Unix(tokens.ExpiresAt, 0).Format(time.RFC3339),
		strings.Join(tokens.Scopes, ", "),
	)
	return err
}

func handleLogoutCommand(_ []string, slash slashContext) error {
	manager, err := appauth.NewManager(*slash.cfg, nil)
	if err != nil {
		return err
	}
	if err := manager.Logout(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(slash.streams.Out, "logout successful")
	return err
}

func executeSlashLikeCommand(ctx context.Context, name string, args []string, slash slashContext) (string, error) {
	if slash.skillManager != nil {
		return NewCombinedRegistryAdapter(commandRegistry, slash.skillManager, slash.defaultWorkDir, slash).Execute(ctx, name, args)
	}
	return NewRegistryAdapter(commandRegistry, slash).Execute(ctx, name, args)
}

func executeGeneratedSkillPrompt(ctx context.Context, prompt string, slash slashContext) error {
	runner, err := slash.newEngineForDir(slash.defaultWorkDir)
	if err != nil {
		return err
	}
	session := state.NewSession(slash.defaultWorkDir)
	result, err := runner.RunGeneratedPrompt(ctx, session, prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result.Output) != "" {
		if _, err := fmt.Fprintln(slash.streams.Out, result.Output); err != nil {
			return err
		}
	}
	if slash.saveSession {
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
	}
	return nil
}

func handleMCPCommand(args []string, slash slashContext) error {
	switch {
	case len(args) == 0 || args[0] == "list":
		if len(slash.cfg.MCPServers) == 0 {
			_, err := fmt.Fprintln(slash.streams.Out, "no mcp servers configured")
			return err
		}
		lines := make([]string, 0, len(slash.cfg.MCPServers))
		for _, server := range slash.cfg.MCPServers {
			target := strings.Join(server.Command, " ")
			if server.URL != "" {
				target = server.URL
			}
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", server.Name, server.Transport, target))
		}
		_, err := fmt.Fprintln(slash.streams.Out, strings.Join(lines, "\n"))
		return err
	case args[0] == "add" && len(args) >= 3:
		server, err := parseMCPServerArgs(args[1], args[2:])
		if err != nil {
			return err
		}
		upsertMCPServer(&slash.cfg.MCPServers, server)
		if err := config.Save(*slash.cfg); err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "added mcp server %s\n", server.Name)
		return err
	case args[0] == "remove" && len(args) >= 2:
		if !removeMCPServer(&slash.cfg.MCPServers, args[1]) {
			return fmt.Errorf("mcp server %s is not configured", args[1])
		}
		if err := config.Save(*slash.cfg); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "removed mcp server %s\n", args[1])
		return err
	default:
		return fmt.Errorf("usage: /mcp [list|add <name> -- <command...>|add <name> --url <url>|remove <name>]")
	}
}

func handleStatusCommand(_ []string, slash slashContext) error {
	var lines []string
	lines = append(lines,
		"cwd: "+slash.defaultWorkDir,
		"backend: "+slash.cfg.Backend,
		"provider: "+slash.cfg.Provider,
		"model: "+slash.cfg.Model,
		"permission_mode: "+slash.cfg.PermissionMode,
		"theme: "+slash.cfg.Theme,
		fmt.Sprintf("mcp_servers: %d", len(slash.cfg.MCPServers)),
	)

	if slash.skillManager != nil {
		stats := slash.skillManager.GetStats()
		lines = append(lines, fmt.Sprintf("skills: %d total, %d user-invocable", stats.TotalSkills, stats.UserInvocable))
	}

	if branch := gitOutput(slash.defaultWorkDir, "branch", "--show-current"); branch != "" {
		lines = append(lines, "git_branch: "+branch)
	}
	if changes := gitChangedCount(slash.defaultWorkDir); changes >= 0 {
		lines = append(lines, fmt.Sprintf("git_changes: %d", changes))
	}

	pluginCount, disabledCount := pluginStatusCounts(slash)
	lines = append(lines, fmt.Sprintf("plugins: %d configured, %d disabled", pluginCount, disabledCount))

	authStatus := "not authenticated"
	if manager, err := appauth.NewManager(*slash.cfg, nil); err == nil {
		if status, err := manager.Status(context.Background()); err == nil && status.Authenticated {
			authStatus = "authenticated"
		}
	}
	lines = append(lines, "auth: "+authStatus)

	_, err := fmt.Fprintln(slash.streams.Out, strings.Join(lines, "\n"))
	return err
}

func handleFilesCommand(args []string, slash slashContext) error {
	mode := "tracked"
	if len(args) > 0 {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}

	var files []string
	var err error
	switch mode {
	case "", "tracked":
		files, err = gitTrackedFiles(slash.defaultWorkDir)
		if err != nil {
			files, err = walkProjectFiles(slash.defaultWorkDir)
		}
	case "changed":
		files, err = gitChangedFiles(slash.defaultWorkDir)
	case "all":
		files, err = walkProjectFiles(slash.defaultWorkDir)
	default:
		return fmt.Errorf("usage: /files [tracked|changed|all]")
	}
	if err != nil {
		return err
	}
	if len(files) == 0 {
		_, err = fmt.Fprintln(slash.streams.Out, "no files")
		return err
	}
	sort.Strings(files)
	_, err = fmt.Fprintln(slash.streams.Out, strings.Join(files, "\n"))
	return err
}

func handlePermissionsCommand(args []string, slash slashContext) error {
	if len(args) == 0 || args[0] == "list" {
		return listPermissions(slash)
	}
	if len(args) < 3 {
		return fmt.Errorf("usage: /permissions [list|add <allow|deny|ask> <rule>|remove <allow|deny|ask> <rule>]")
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	behavior := strings.ToLower(strings.TrimSpace(args[1]))
	if !isPermissionBehavior(behavior) {
		return fmt.Errorf("permission behavior must be allow, deny, or ask")
	}
	rule := strings.TrimSpace(strings.Join(args[2:], " "))
	if rule == "" {
		return fmt.Errorf("permission rule is required")
	}

	switch action {
	case "add":
		if err := updateUserPermissionRules(slash.defaultWorkDir, behavior, rule, true); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "added %s permission: %s\n", behavior, rule)
		return err
	case "remove":
		if err := updateUserPermissionRules(slash.defaultWorkDir, behavior, rule, false); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "removed %s permission: %s\n", behavior, rule)
		return err
	default:
		return fmt.Errorf("usage: /permissions [list|add <allow|deny|ask> <rule>|remove <allow|deny|ask> <rule>]")
	}
}

func handlePluginCommand(args []string, slash slashContext) error {
	if len(args) == 0 || args[0] == "list" || args[0] == "status" {
		return listPlugins(slash)
	}

	switch args[0] {
	case "install-local":
		if len(args) < 3 {
			return fmt.Errorf("usage: /plugin install-local <plugin-id> <path>")
		}
		installer := plugins.Installer{
			CacheDir:       filepath.Join(slash.home, "plugins", "cache"),
			InstalledStore: plugins.InstalledPluginStore{Path: installedPluginsPath(slash.home)},
		}
		info, err := installer.InstallLocal(args[1], args[2], plugins.PluginScopeUser, slash.defaultWorkDir)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "installed plugin %s at %s\n", info.PluginID, info.InstallPath)
		return err
	case "uninstall":
		if len(args) < 2 {
			return fmt.Errorf("usage: /plugin uninstall <plugin-id>")
		}
		installer := plugins.Installer{InstalledStore: plugins.InstalledPluginStore{Path: installedPluginsPath(slash.home)}}
		if err := installer.Uninstall(args[1], appsettings.SettingsFilePathForSource(appsettings.SourceUser, slash.defaultWorkDir)); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "uninstalled plugin %s\n", args[1])
		return err
	case "enable", "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: /plugin %s <plugin-id>", args[0])
		}
		enabled := args[0] == "enable"
		if err := plugins.UpdateEnabledPluginSetting(appsettings.SettingsFilePathForSource(appsettings.SourceUser, slash.defaultWorkDir), args[1], enabled); err != nil {
			return err
		}
		word := "disabled"
		if enabled {
			word = "enabled"
		}
		_, err := fmt.Fprintf(slash.streams.Out, "%s plugin %s\n", word, args[1])
		return err
	default:
		return fmt.Errorf("usage: /plugin [list|install-local <id> <path>|uninstall <id>|enable <id>|disable <id>]")
	}
}

func handleBranchCommand(args []string, slash slashContext) error {
	action := "show"
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch action {
	case "", "show", "current":
		branch, err := gitCommandOutput(slash.defaultWorkDir, "branch", "--show-current")
		if err != nil {
			return err
		}
		branch = strings.TrimSpace(branch)
		if branch == "" {
			_, err = fmt.Fprintln(slash.streams.Out, "current_branch: detached")
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "current_branch: %s\n", branch)
		return err
	case "list":
		output, err := gitCommandOutput(slash.defaultWorkDir, "branch", "--list", "--format=%(refname:short)")
		if err != nil {
			return err
		}
		branches := splitNonEmptyLines(output)
		if len(branches) == 0 {
			_, err = fmt.Fprintln(slash.streams.Out, "no branches")
			return err
		}
		sort.Strings(branches)
		_, err = fmt.Fprintln(slash.streams.Out, strings.Join(branches, "\n"))
		return err
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: /branch create <name>")
		}
		if _, err := gitCommandOutput(slash.defaultWorkDir, "branch", args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "created branch %s\n", args[1])
		return err
	case "switch", "checkout":
		if len(args) < 2 {
			return fmt.Errorf("usage: /branch switch <name>")
		}
		if _, err := gitCommandOutput(slash.defaultWorkDir, "switch", args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "switched to branch %s\n", args[1])
		return err
	case "delete", "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: /branch delete <name>")
		}
		if _, err := gitCommandOutput(slash.defaultWorkDir, "branch", "-d", args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(slash.streams.Out, "deleted branch %s\n", args[1])
		return err
	default:
		return fmt.Errorf("usage: /branch [show|list|create <name>|switch <name>|delete <name>]")
	}
}

func handleTagCommand(args []string, slash slashContext) error {
	action := "list"
	rest := args
	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "list", "show":
			action = "list"
			rest = args[1:]
		case "add", "remove":
			action = strings.ToLower(strings.TrimSpace(args[0]))
			rest = args[1:]
		}
	}

	switch action {
	case "list":
		if len(rest) > 1 {
			return fmt.Errorf("usage: /tag list [<session-id>|latest]")
		}
		session, err := resolveSession(rest, slash.home)
		if err != nil {
			return err
		}
		tags := "(none)"
		if len(session.Tags) > 0 {
			tags = strings.Join(session.Tags, ", ")
		}
		_, err = fmt.Fprintf(slash.streams.Out, "session_id: %s\ntags: %s\n", session.ID, tags)
		return err
	case "add", "remove":
		if len(rest) < 1 {
			return fmt.Errorf("usage: /tag %s <tag> [<session-id>|latest]", action)
		}
		tag := strings.TrimSpace(rest[0])
		if tag == "" {
			return fmt.Errorf("tag is required")
		}
		session, err := resolveSession(rest[1:], slash.home)
		if err != nil {
			return err
		}
		if action == "add" {
			session.AddTag(tag)
		} else {
			session.RemoveTag(tag)
		}
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
		verb := "added"
		if action == "remove" {
			verb = "removed"
		}
		_, err = fmt.Fprintf(slash.streams.Out, "%s tag %s on session %s\n", verb, tag, session.ID)
		return err
	default:
		return fmt.Errorf("usage: /tag [list|add <tag>|remove <tag>] [<session-id>|latest]")
	}
}

func handleExportCommand(args []string, slash slashContext) error {
	sessionToken, format, outputPath, err := parseExportArgs(args)
	if err != nil {
		return err
	}
	session, err := resolveSession([]string{sessionToken}, slash.home)
	if err != nil {
		return err
	}

	var data []byte
	switch format {
	case "json":
		data, err = session.Export()
	case "markdown":
		data = []byte(formatSessionMarkdown(session))
	default:
		err = fmt.Errorf("unsupported export format: %s", format)
	}
	if err != nil {
		return err
	}

	if outputPath == "" {
		_, err = fmt.Fprintln(slash.streams.Out, strings.TrimRight(string(data), "\n"))
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintf(slash.streams.Out, "exported session %s to %s\n", session.ID, outputPath)
	return err
}

func handleUsageCommand(args []string, slash slashContext) error {
	mode := "summary"
	limit := 10
	if len(args) > 0 {
		if parsed, err := strconv.Atoi(args[0]); err == nil {
			mode = "sessions"
			limit = parsed
		} else {
			mode = strings.ToLower(strings.TrimSpace(args[0]))
		}
	}
	if len(args) > 1 {
		parsed, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid limit: %s", args[1])
		}
		limit = parsed
	}
	if limit <= 0 {
		return fmt.Errorf("limit must be greater than zero")
	}

	sm := state.NewSessionManager(slash.home)
	switch mode {
	case "", "summary":
		stats, err := sm.GetSessionStats()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(
			slash.streams.Out,
			"sessions: %d\narchived_sessions: %d\nmessages: %d\ntotal_tokens: %d\nestimated_cost_usd: %.6f\n",
			stats.TotalSessions,
			stats.ArchivedSessions,
			stats.TotalMessages,
			stats.TotalTokens,
			stats.TotalCost,
		)
		return err
	case "sessions", "list":
		sessions, err := sm.ListSessions(state.SearchOptions{IncludeArchived: true, SortBy: "updated", Limit: limit})
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			_, err = fmt.Fprintln(slash.streams.Out, "no sessions")
			return err
		}
		for _, session := range sessions {
			last := singleLinePreview(session.LastUserMessage(), 64)
			fmt.Fprintf(
				slash.streams.Out,
				"%s  %s  tokens=%d  cost=%.6f  %s\n",
				session.UpdatedAt.Format("2006-01-02 15:04"),
				session.ID,
				session.Usage.TotalTokens,
				session.Usage.EstimatedCostUSD,
				last,
			)
		}
		return nil
	default:
		return fmt.Errorf("usage: /usage [summary|sessions] [<limit>]")
	}
}

func handleCostCommand(args []string, slash slashContext) error {
	session, err := resolveSession(args, slash.home)
	if err != nil {
		return err
	}
	usage := session.Usage
	_, err = fmt.Fprintf(
		slash.streams.Out,
		"session_id: %s\ninput_tokens: %d\noutput_tokens: %d\ntotal_tokens: %d\nestimated_cost_usd: %.6f\n",
		session.ID,
		usage.InputTokens,
		usage.OutputTokens,
		usage.TotalTokens,
		usage.EstimatedCostUSD,
	)
	return err
}

func handleMemoryCommand(args []string, slash slashContext) error {
	mgr := memory.NewLegacyManager(slash.home)

	if len(args) == 0 || args[0] == "show" {
		name := "default"
		if len(args) > 1 {
			name = args[1]
		}
		content, err := mgr.Read(name)
		if err != nil {
			if os.IsNotExist(err) {
				_, writeErr := fmt.Fprintln(slash.streams.Out, "")
				return writeErr
			}
			return err
		}
		_, err = fmt.Fprintln(slash.streams.Out, strings.TrimRight(content, "\n"))
		return err
	}

	switch args[0] {
	case "list":
		files, err := mgr.List()
		if err != nil {
			return err
		}
		if len(files) == 0 {
			_, err = fmt.Fprintln(slash.streams.Out, "no memory files")
			return err
		}
		for _, f := range files {
			fmt.Fprintln(slash.streams.Out, f)
		}
		return nil

	case "append":
		if len(args) < 2 {
			return fmt.Errorf("usage: /memory append [--file <name>] <text>")
		}
		name, text := "default", strings.Join(args[1:], " ")
		if args[1] == "--file" && len(args) > 3 {
			name, text = args[2], strings.Join(args[3:], " ")
		}
		if err := mgr.Append(name, text); err != nil {
			return err
		}
		_, err := fmt.Fprintln(slash.streams.Out, "memory updated")
		return err

	case "edit":
		if len(args) < 4 {
			return fmt.Errorf("usage: /memory edit <file> <line> <new-text>")
		}
		lineNum, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("invalid line number: %s", args[2])
		}
		if err := mgr.Edit(args[1], lineNum, strings.Join(args[3:], " ")); err != nil {
			return err
		}
		_, err = fmt.Fprintln(slash.streams.Out, "memory updated")
		return err

	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: /memory delete <file> [<line>]")
		}
		if len(args) == 2 {
			if err := mgr.DeleteFile(args[1]); err != nil {
				return err
			}
			_, err := fmt.Fprintf(slash.streams.Out, "deleted memory file: %s\n", args[1])
			return err
		}
		lineNum, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("invalid line number: %s", args[2])
		}
		if err := mgr.Delete(args[1], lineNum); err != nil {
			return err
		}
		_, err = fmt.Fprintln(slash.streams.Out, "memory updated")
		return err

	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: /memory search <query>")
		}
		query := strings.Join(args[1:], " ")
		results, err := mgr.Search(query)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			_, err = fmt.Fprintln(slash.streams.Out, "no results found")
			return err
		}
		for file, lines := range results {
			fmt.Fprintf(slash.streams.Out, "%s: lines %v\n", file, lines)
		}
		return nil

	case "stats":
		files, err := mgr.List()
		if err != nil {
			return err
		}
		size, err := mgr.Size()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "memory files: %d, total size: %d bytes\n", len(files), size)
		return err

	default:
		return fmt.Errorf("usage: /memory [show|list|append|edit|delete|search|stats]")
	}
}

func handleResumeCommand(ctx context.Context, args []string, slash slashContext) error {
	// Parse --from-turn flag if present
	var fromTurn int = -1
	var filteredArgs []string

	for i := 0; i < len(args); i++ {
		if args[i] == "--from-turn" && i+1 < len(args) {
			var err error
			fromTurn, err = strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid turn number: %s", args[i+1])
			}
			i++ // skip the turn number
		} else {
			filteredArgs = append(filteredArgs, args[i])
		}
	}

	session, prompt, err := loadResumeTarget(filteredArgs, slash.home)
	if err != nil {
		return err
	}

	// If --from-turn specified, truncate messages to that point
	if fromTurn >= 0 {
		if fromTurn >= len(session.Messages) {
			return fmt.Errorf("turn number %d exceeds session length (%d messages)", fromTurn, len(session.Messages))
		}
		session.Messages = session.Messages[:fromTurn+1]

		// Recalculate usage
		session.Usage = state.Usage{}
		for _, msg := range session.Messages {
			if msg.Role == "user" {
				session.Usage.RecordInput(msg.Content)
			} else if msg.Role == "assistant" {
				session.Usage.RecordOutput(msg.Content)
			} else if msg.Role == "tool" {
				session.Usage.RecordOutput(msg.ToolOutput)
			}
		}
	}

	if prompt == "" {
		_, err := fmt.Fprintf(
			slash.streams.Out,
			"session_id: %s\nmessages: %d\nupdated_at: %s\nlast_user_message: %s\n",
			session.ID,
			len(session.Messages),
			session.UpdatedAt.Format(time.RFC3339),
			session.LastUserMessage(),
		)
		return err
	}

	workDir := session.WorkingDir
	if workDir == "" {
		workDir = slash.defaultWorkDir
	}
	runner, err := slash.newEngineForDir(workDir)
	if err != nil {
		return err
	}
	result, err := runner.Run(ctx, session, prompt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result.Output) != "" {
		if _, err := fmt.Fprintln(slash.streams.Out, result.Output); err != nil {
			return err
		}
	}
	if slash.saveSession {
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
	}
	return nil
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "backend":
		cfg.Backend = value
	case "provider":
		cfg.Provider = value
	case "model":
		cfg.Model = value
	case "permission_mode":
		cfg.PermissionMode = value
	case "theme":
		cfg.Theme = value
	case "api_base_url":
		cfg.APIBaseURL = value
	case "api_key":
		cfg.APIKey = value
	case "api_token":
		cfg.APIToken = value
	case "timeout_seconds":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.TimeoutSeconds = parsed
	case "max_turns":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.MaxTurns = parsed
	case "secret_store":
		cfg.SecretStore = value
	case "plugin_dir":
		cfg.PluginDir = value
	case "bridge_secret":
		cfg.BridgeSecret = value
	case "telemetry.enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Telemetry.Enabled = parsed
	case "telemetry.insecure":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Telemetry.Insecure = parsed
	case "telemetry.endpoint":
		cfg.Telemetry.Endpoint = value
	case "telemetry.exporter":
		cfg.Telemetry.Exporter = value
	case "telemetry.service_name":
		cfg.Telemetry.ServiceName = value
	case "oauth.client_id":
		cfg.OAuth.ClientID = value
	case "oauth.client_secret":
		cfg.OAuth.ClientSecret = value
	case "oauth.auth_url":
		cfg.OAuth.AuthURL = value
	case "oauth.token_url":
		cfg.OAuth.TokenURL = value
	case "oauth.redirect_host":
		cfg.OAuth.RedirectHost = value
	case "oauth.redirect_port":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.OAuth.RedirectPort = parsed
	case "oauth.scopes":
		cfg.OAuth.Scopes = splitAndTrimCSV(value)
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return nil
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func gitChangedCount(dir string) int {
	files, err := gitChangedFiles(dir)
	if err != nil {
		return -1
	}
	return len(files)
}

func gitTrackedFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(string(output)), nil
}

func gitChangedFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range splitNonEmptyLines(string(output)) {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		if path != "" {
			files = append(files, path)
		}
	}
	return files, nil
}

func walkProjectFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", ".claude", ".gocache", ".gomodcache", "node_modules", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func listPermissions(slash slashContext) error {
	merged := appsettings.LoadMergedSettings(slash.defaultWorkDir)
	if len(merged.Errors) > 0 {
		return fmt.Errorf("cannot read invalid settings: %s", merged.Errors[0].Message)
	}

	perms := permissionDocument(merged.Settings)
	var b strings.Builder
	fmt.Fprintf(&b, "permission_mode: %s\n", slash.cfg.PermissionMode)
	if defaultMode, ok := perms["defaultMode"].(string); ok && defaultMode != "" {
		fmt.Fprintf(&b, "settings_default_mode: %s\n", defaultMode)
	}
	for _, behavior := range []string{"allow", "deny", "ask"} {
		rules := stringSliceFromAny(perms[behavior])
		b.WriteString(behavior + ":\n")
		if len(rules) == 0 {
			b.WriteString("  (none)\n")
			continue
		}
		sort.Strings(rules)
		for _, rule := range rules {
			fmt.Fprintf(&b, "  - %s\n", rule)
		}
	}
	_, err := fmt.Fprint(slash.streams.Out, b.String())
	return err
}

func updateUserPermissionRules(workingDir string, behavior string, rule string, add bool) error {
	userSettings := appsettings.LoadSettingsForSource(appsettings.SourceUser, workingDir)
	if len(userSettings.Errors) > 0 {
		return fmt.Errorf("cannot update invalid user settings: %s", userSettings.Errors[0].Message)
	}

	perms := permissionDocument(userSettings.Settings)
	rules := stringSliceFromAny(perms[behavior])
	seen := make(map[string]bool, len(rules)+1)
	next := make([]any, 0, len(rules)+1)
	for _, existing := range rules {
		if existing == rule {
			seen[existing] = true
			if add {
				next = append(next, existing)
			}
			continue
		}
		if !seen[existing] {
			next = append(next, existing)
			seen[existing] = true
		}
	}
	if add && !seen[rule] {
		next = append(next, rule)
	}

	update := appsettings.Document{
		"permissions": appsettings.Document{
			behavior: next,
		},
	}
	return appsettings.UpdateSettingsForSource(appsettings.EditableUser, workingDir, update)
}

func permissionDocument(doc appsettings.Document) map[string]any {
	if doc == nil {
		return map[string]any{}
	}
	switch perms := doc["permissions"].(type) {
	case map[string]any:
		return perms
	case appsettings.Document:
		return map[string]any(perms)
	default:
		return map[string]any{}
	}
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return nil
	}
}

func isPermissionBehavior(value string) bool {
	switch value {
	case "allow", "deny", "ask":
		return true
	default:
		return false
	}
}

func pluginStatusCounts(slash slashContext) (int, int) {
	loaded, _ := loadConfiguredPlugins(slash)
	disabled := 0
	for _, plugin := range loaded {
		if !plugin.Enabled {
			disabled++
		}
	}
	return len(loaded), disabled
}

func listPlugins(slash slashContext) error {
	loaded, err := loadConfiguredPlugins(slash)
	if err != nil {
		return err
	}
	if len(loaded) == 0 {
		_, err = fmt.Fprintln(slash.streams.Out, "no plugins configured")
		return err
	}
	lines := make([]string, 0, len(loaded))
	for _, plugin := range loaded {
		status := "enabled"
		if !plugin.Enabled {
			status = "disabled"
		}
		version := plugin.Manifest.Version
		if version == "" {
			version = "unversioned"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", plugin.Source, version, status, plugin.Path))
	}
	sort.Strings(lines)
	_, err = fmt.Fprintln(slash.streams.Out, strings.Join(lines, "\n"))
	return err
}

func loadConfiguredPlugins(slash slashContext) ([]*plugins.LoadedPlugin, error) {
	enabled := enabledPluginSettings(slash.defaultWorkDir)
	var loaded []*plugins.LoadedPlugin
	if strings.TrimSpace(slash.cfg.PluginDir) != "" {
		fromDir, err := plugins.NewLoader(slash.cfg.PluginDir).LoadDetailed(plugins.LoadOptions{
			Marketplace:     InlinePluginMarketplaceForCLI(),
			Repository:      "plugin_dir",
			EnabledPlugins:  enabled,
			IncludeDisabled: true,
		})
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, fromDir...)
	}
	registry, err := (plugins.InstalledPluginStore{Path: installedPluginsPath(slash.home)}).Load()
	if err != nil {
		return nil, err
	}
	installed, err := plugins.LoadInstalledPlugins(registry, enabled)
	if err != nil {
		return nil, err
	}
	loaded = append(loaded, installed...)
	return loaded, nil
}

func enabledPluginSettings(workingDir string) map[string]bool {
	merged := appsettings.LoadMergedSettings(workingDir)
	raw, ok := merged.Settings["enabledPlugins"].(map[string]any)
	if !ok {
		if doc, ok := merged.Settings["enabledPlugins"].(appsettings.Document); ok {
			raw = map[string]any(doc)
		}
	}
	out := map[string]bool{}
	for id, value := range raw {
		if enabled, ok := value.(bool); ok {
			out[id] = enabled
		}
	}
	return out
}

func installedPluginsPath(home string) string {
	return filepath.Join(home, "installed_plugins.json")
}

func gitCommandOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func parseExportArgs(args []string) (sessionToken string, format string, outputPath string, err error) {
	sessionToken = "latest"
	format = "json"
	if len(args) == 0 {
		return sessionToken, format, "", nil
	}

	if normalized, ok := normalizeExportFormat(args[0]); ok {
		format = normalized
		if len(args) > 1 {
			outputPath = args[1]
		}
		if len(args) > 2 {
			return "", "", "", fmt.Errorf("usage: /export [<session-id>|latest] [json|markdown] [<output-path>]")
		}
		return sessionToken, format, outputPath, nil
	}

	sessionToken = args[0]
	if len(args) > 1 {
		if normalized, ok := normalizeExportFormat(args[1]); ok {
			format = normalized
			if len(args) > 2 {
				outputPath = args[2]
			}
			if len(args) > 3 {
				return "", "", "", fmt.Errorf("usage: /export [<session-id>|latest] [json|markdown] [<output-path>]")
			}
			return sessionToken, format, outputPath, nil
		}
		outputPath = args[1]
	}
	if len(args) > 2 {
		return "", "", "", fmt.Errorf("usage: /export [<session-id>|latest] [json|markdown] [<output-path>]")
	}
	return sessionToken, format, outputPath, nil
}

func normalizeExportFormat(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "json":
		return "json", true
	case "markdown", "md":
		return "markdown", true
	default:
		return "", false
	}
}

func formatSessionMarkdown(session *state.Session) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Claude Codex Session %s\n\n", session.ID)
	fmt.Fprintf(&out, "- Working dir: %s\n", session.WorkingDir)
	fmt.Fprintf(&out, "- Started: %s\n", session.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&out, "- Updated: %s\n", session.UpdatedAt.Format(time.RFC3339))
	fmt.Fprintf(&out, "- Messages: %d\n", len(session.Messages))
	fmt.Fprintf(&out, "- Total tokens: %d\n", session.Usage.TotalTokens)
	if len(session.Tags) > 0 {
		fmt.Fprintf(&out, "- Tags: %s\n", strings.Join(session.Tags, ", "))
	}
	if session.Description != "" {
		fmt.Fprintf(&out, "- Description: %s\n", session.Description)
	}
	out.WriteString("\n")

	for i, msg := range session.Messages {
		role := msg.Role
		if msg.Hidden {
			role += " hidden"
		}
		fmt.Fprintf(&out, "## %d. %s\n\n", i+1, role)
		if msg.ToolName != "" {
			fmt.Fprintf(&out, "Tool: %s\n\n", msg.ToolName)
		}
		content := msg.Content
		if msg.Role == "tool" && content == "" {
			content = msg.ToolOutput
		}
		content = strings.TrimSpace(content)
		if content == "" {
			content = "(empty)"
		}
		out.WriteString(content)
		out.WriteString("\n\n")
	}
	return out.String()
}

func singleLinePreview(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "(no user message)"
	}
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func formatToolCheck(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Sprintf("%s: missing", name)
	}
	return fmt.Sprintf("%s: ok (%s)", name, path)
}

func resolveSession(args []string, home string) (*state.Session, error) {
	if len(args) == 0 || args[0] == "latest" {
		return state.LoadLatestSession(home)
	}
	return state.LoadSession(home, args[0])
}

func parseMCPServerArgs(name string, args []string) (config.MCPServerConfig, error) {
	if strings.TrimSpace(name) == "" {
		return config.MCPServerConfig{}, fmt.Errorf("mcp server name is required")
	}
	switch {
	case len(args) >= 2 && args[0] == "--url":
		return config.MCPServerConfig{Name: name, Transport: "sse", URL: args[1]}, nil
	case len(args) == 1 && args[0] == "--url":
		return config.MCPServerConfig{}, mcpUsageError(name)
	case len(args) >= 2 && args[0] == "--":
		return config.MCPServerConfig{Name: name, Transport: "stdio", Command: append([]string(nil), args[1:]...)}, nil
	case len(args) == 1 && args[0] == "--":
		return config.MCPServerConfig{}, mcpUsageError(name)
	case len(args) >= 1:
		return config.MCPServerConfig{Name: name, Transport: "stdio", Command: append([]string(nil), args...)}, nil
	default:
		return config.MCPServerConfig{}, mcpUsageError(name)
	}
}

func mcpUsageError(name string) error {
	return fmt.Errorf("usage: /mcp add %s -- <command...> or /mcp add %s --url <url>", name, name)
}

func splitAndTrimCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func upsertMCPServer(servers *[]config.MCPServerConfig, server config.MCPServerConfig) {
	for i := range *servers {
		if (*servers)[i].Name == server.Name {
			(*servers)[i] = server
			return
		}
	}
	*servers = append(*servers, server)
}

func removeMCPServer(servers *[]config.MCPServerConfig, name string) bool {
	for i := range *servers {
		if (*servers)[i].Name != name {
			continue
		}
		*servers = append((*servers)[:i], (*servers)[i+1:]...)
		return true
	}
	return false
}

func loadResumeTarget(args []string, home string) (*state.Session, string, error) {
	switch {
	case len(args) == 0:
		session, err := state.LoadLatestSession(home)
		return session, "", err
	case args[0] == "latest":
		session, err := state.LoadLatestSession(home)
		if err != nil {
			return nil, "", err
		}
		return session, strings.Join(args[1:], " "), nil
	default:
		session, err := state.LoadSession(home, args[0])
		if err != nil {
			return nil, "", err
		}
		return session, strings.Join(args[1:], " "), nil
	}
}

func handleHelpCommand(slash slashContext) error {
	help := generateHelpForCommands(listCommandsForHelp(slash))
	_, err := fmt.Fprint(slash.streams.Out, help)
	return err
}

func listCommandsForHelp(slash slashContext) []*Command {
	commands := commandRegistry.List()
	if slash.skillManager == nil {
		return commands
	}

	merged := make([]*Command, 0, len(commands)+8)
	seen := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		merged = append(merged, cmd)
		seen[cmd.Name] = true
	}

	for _, cmd := range NewSkillCommandRegistry(slash.skillManager, slash.defaultWorkDir).List() {
		if seen[cmd.Name] {
			continue
		}
		merged = append(merged, cmd)
		seen[cmd.Name] = true
	}

	return merged
}

func isModelHelpArg(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "help", "-h", "--help":
		return true
	default:
		return false
	}
}

func isModelInfoArg(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "show", "current", "info":
		return true
	default:
		return false
	}
}

func handleHistoryCommand(args []string, slash slashContext) error {
	limit := 10
	if len(args) > 0 {
		parsed, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid limit: %s", args[0])
		}
		limit = parsed
	}

	sessionsDir := filepath.Join(slash.home, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			_, writeErr := fmt.Fprintln(slash.streams.Out, "no session history found")
			return writeErr
		}
		return err
	}

	// Sort by modification time (most recent first)
	type sessionInfo struct {
		id      string
		modTime time.Time
	}
	var sessions []sessionInfo
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			sessions = append(sessions, sessionInfo{
				id:      strings.TrimSuffix(entry.Name(), ".json"),
				modTime: info.ModTime(),
			})
		}
	}

	// Sort by time descending
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].modTime.After(sessions[i].modTime) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}

	// Limit results
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}

	if len(sessions) == 0 {
		_, err := fmt.Fprintln(slash.streams.Out, "no sessions found")
		return err
	}

	var output strings.Builder
	for _, s := range sessions {
		session, err := state.LoadSession(slash.home, s.id)
		if err != nil {
			continue
		}
		lastMsg := session.LastUserMessage()
		if len(lastMsg) > 60 {
			lastMsg = lastMsg[:60] + "..."
		}
		fmt.Fprintf(&output, "%s  %s  %s\n",
			s.modTime.Format("2006-01-02 15:04"),
			s.id[:8],
			lastMsg,
		)
	}

	_, err = fmt.Fprint(slash.streams.Out, output.String())
	return err
}

func handleDiffCommand(args []string, slash slashContext) error {
	var cmd *exec.Cmd
	if len(args) > 0 {
		// Diff specific path
		cmd = exec.Command("git", "diff", args[0])
	} else {
		// Diff all changes
		cmd = exec.Command("git", "diff")
	}
	cmd.Dir = slash.defaultWorkDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git diff failed: %w", err)
	}

	if len(output) == 0 {
		_, err := fmt.Fprintln(slash.streams.Out, "no changes")
		return err
	}

	_, err = fmt.Fprint(slash.streams.Out, string(output))
	return err
}

func handleCommitCommand(args []string, slash slashContext) error {
	// Check if there are staged changes
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = slash.defaultWorkDir
	if err := cmd.Run(); err == nil {
		_, err := fmt.Fprintln(slash.streams.Out, "no staged changes to commit")
		return err
	}

	// Get diff of staged changes
	cmd = exec.Command("git", "diff", "--cached")
	cmd.Dir = slash.defaultWorkDir
	diff, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get staged changes: %w", err)
	}

	// For now, just show the diff and prompt user to commit manually
	// TODO: Integrate with AI to generate commit message
	_, err = fmt.Fprintf(slash.streams.Out, "staged changes:\n%s\nuse 'git commit' to commit these changes\n", string(diff))
	return err
}

func handleReviewCommand(args []string, slash slashContext) error {
	// Run basic checks
	var output strings.Builder

	// Check git status
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = slash.defaultWorkDir
	statusOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git status failed: %w", err)
	}
	fmt.Fprintf(&output, "git status:\n%s\n", string(statusOut))

	// Run go vet
	cmd = exec.Command("go", "vet", "./...")
	cmd.Dir = slash.defaultWorkDir
	vetOut, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(&output, "go vet found issues:\n%s\n", string(vetOut))
	} else {
		fmt.Fprintf(&output, "go vet: ok\n")
	}

	// Run tests
	cmd = exec.Command("go", "test", "./...")
	cmd.Dir = slash.defaultWorkDir
	testOut, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(&output, "tests failed:\n%s\n", string(testOut))
	} else {
		fmt.Fprintf(&output, "tests: ok\n")
	}

	_, err = fmt.Fprint(slash.streams.Out, output.String())
	return err
}

func handleCompactCommand(args []string, slash slashContext) error {
	session, err := resolveSession(args, slash.home)
	if err != nil {
		return err
	}

	// Count messages before compaction
	beforeCount := len(session.Messages)

	// Simple compaction: keep only the last 10 messages
	// TODO: Implement smarter compaction with AI summarization
	if len(session.Messages) > 10 {
		session.Messages = session.Messages[len(session.Messages)-10:]
	}

	// Save compacted session
	if _, err := session.Save(slash.home); err != nil {
		return err
	}

	afterCount := len(session.Messages)
	_, err = fmt.Fprintf(slash.streams.Out, "compacted session %s: %d -> %d messages\n", session.ID, beforeCount, afterCount)
	return err
}

func handleSessionCommand(args []string, slash slashContext) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /session [tag|search|branch|export|import|archive|cleanup|stats]")
	}

	sm := state.NewSessionManager(slash.home)
	subcommand := args[0]

	switch subcommand {
	case "tag":
		return handleSessionTag(args[1:], slash, sm)
	case "search":
		return handleSessionSearch(args[1:], slash, sm)
	case "branch":
		return handleSessionBranch(args[1:], slash, sm)
	case "export":
		return handleSessionExport(args[1:], slash, sm)
	case "import":
		return handleSessionImport(args[1:], slash, sm)
	case "archive":
		return handleSessionArchive(args[1:], slash, sm)
	case "cleanup":
		return handleSessionCleanup(args[1:], slash, sm)
	case "stats":
		return handleSessionStats(slash, sm)
	default:
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

func handleSessionTag(args []string, slash slashContext, sm *state.SessionManager) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: /session tag <session-id|latest> <add|remove> <tag>")
	}

	session, err := resolveSession(args[:1], slash.home)
	if err != nil {
		return err
	}

	action := args[1]
	if len(args) < 3 {
		return fmt.Errorf("tag name required")
	}
	tag := args[2]

	switch action {
	case "add":
		session.AddTag(tag)
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "added tag '%s' to session %s\n", tag, session.ID)
		return err
	case "remove":
		session.RemoveTag(tag)
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "removed tag '%s' from session %s\n", tag, session.ID)
		return err
	default:
		return fmt.Errorf("unknown action: %s (use 'add' or 'remove')", action)
	}
}

func handleSessionSearch(args []string, slash slashContext, sm *state.SessionManager) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /session search <query>")
	}

	query := strings.Join(args, " ")
	sessions, err := sm.SearchSessions(query, state.SearchOptions{})
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		_, err = fmt.Fprintln(slash.streams.Out, "no sessions found")
		return err
	}

	for _, s := range sessions {
		tags := ""
		if len(s.Tags) > 0 {
			tags = fmt.Sprintf(" [%s]", strings.Join(s.Tags, ", "))
		}
		fmt.Fprintf(slash.streams.Out, "%s: %s%s (%d messages)\n", s.ID, s.Description, tags, len(s.Messages))
	}
	return nil
}

func handleSessionBranch(args []string, slash slashContext, sm *state.SessionManager) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: /session branch <session-id|latest> <turn-number> [description]")
	}

	session, err := resolveSession(args[:1], slash.home)
	if err != nil {
		return err
	}

	turnNum, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid turn number: %s", args[1])
	}

	description := "branched session"
	if len(args) > 2 {
		description = strings.Join(args[2:], " ")
	}

	branch, err := session.Branch(turnNum, description)
	if err != nil {
		return err
	}

	if _, err := branch.Save(slash.home); err != nil {
		return err
	}

	_, err = fmt.Fprintf(slash.streams.Out, "created branch %s from session %s at turn %d\n", branch.ID, session.ID, turnNum)
	return err
}

func handleSessionExport(args []string, slash slashContext, sm *state.SessionManager) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: /session export <session-id|latest> <output-path>")
	}

	session, err := resolveSession(args[:1], slash.home)
	if err != nil {
		return err
	}

	outputPath := args[1]
	if err := sm.ExportSession(session.ID, outputPath); err != nil {
		return err
	}

	_, err = fmt.Fprintf(slash.streams.Out, "exported session %s to %s\n", session.ID, outputPath)
	return err
}

func handleSessionImport(args []string, slash slashContext, sm *state.SessionManager) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: /session import <input-path>")
	}

	inputPath := args[0]
	session, err := sm.ImportSessionFromFile(inputPath)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(slash.streams.Out, "imported session %s from %s\n", session.ID, inputPath)
	return err
}

func handleSessionArchive(args []string, slash slashContext, sm *state.SessionManager) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: /session archive <session-id|latest> [unarchive]")
	}

	session, err := resolveSession(args[:1], slash.home)
	if err != nil {
		return err
	}

	unarchive := len(args) > 1 && args[1] == "unarchive"

	if unarchive {
		session.Unarchive()
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "unarchived session %s\n", session.ID)
	} else {
		session.Archive()
		if _, err := session.Save(slash.home); err != nil {
			return err
		}
		_, err = fmt.Fprintf(slash.streams.Out, "archived session %s\n", session.ID)
	}
	return err
}

func handleSessionCleanup(args []string, slash slashContext, sm *state.SessionManager) error {
	days := 30
	if len(args) > 0 {
		var err error
		days, err = strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid days: %s", args[0])
		}
	}

	count, err := sm.CleanupSessions(time.Duration(days) * 24 * time.Hour)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(slash.streams.Out, "cleaned up %d archived sessions older than %d days\n", count, days)
	return err
}

func handleSessionStats(slash slashContext, sm *state.SessionManager) error {
	stats, err := sm.GetSessionStats()
	if err != nil {
		return err
	}

	fmt.Fprintf(slash.streams.Out, "Session Statistics:\n")
	fmt.Fprintf(slash.streams.Out, "  Total sessions: %d\n", stats.TotalSessions)
	fmt.Fprintf(slash.streams.Out, "  Archived: %d\n", stats.ArchivedSessions)
	fmt.Fprintf(slash.streams.Out, "  Total messages: %d\n", stats.TotalMessages)
	fmt.Fprintf(slash.streams.Out, "  Total tokens: %d\n", stats.TotalTokens)
	fmt.Fprintf(slash.streams.Out, "  Estimated cost: $%.4f\n", stats.TotalCost)

	if len(stats.TagCounts) > 0 {
		fmt.Fprintf(slash.streams.Out, "\nTags:\n")
		for tag, count := range stats.TagCounts {
			fmt.Fprintf(slash.streams.Out, "  %s: %d\n", tag, count)
		}
	}

	return nil
}

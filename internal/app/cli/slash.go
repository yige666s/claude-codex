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
	"strconv"
	"strings"
	"time"

	"github.com/ding/claude-code/claude-go/internal/app/config"
	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	"github.com/ding/claude-code/claude-go/internal/harness/memory"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
)

type slashContext struct {
	cfg             *config.Config
	home            string
	defaultWorkDir  string
	streams         IO
	saveSession     bool
	newEngineForDir func(string) (*engine.Engine, error)
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
			Usage:       "[show|path|set <key> <value>]",
			Description: "Show, get path, or set configuration values",
			Handler:     wrapHandlerWithArgs(handleConfigCommand),
		},
		{
			Name:        "/theme",
			Usage:       "[light|dark]",
			Description: "Get or set UI theme",
			Handler:     wrapHandlerWithArgs(handleThemeCommand),
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
			Aliases:     []string{"/quota", "/usage"},
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

	return commandRegistry.Execute(ctx, fields[0], fields[1:], slash)
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
		return fmt.Errorf("usage: /config [show|path|set <key> <value>]")
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
		fmt.Sprintf("claude-go-home: %s", slash.home),
		fmt.Sprintf("config-path: %s", path),
		fmt.Sprintf("default-workdir: %s", slash.defaultWorkDir),
		fmt.Sprintf("mcp-servers: %d", len(slash.cfg.MCPServers)),
	}
	_, err = fmt.Fprintln(slash.streams.Out, strings.Join(lines, "\n"))
	return err
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
	help := commandRegistry.GenerateHelp()
	_, err := fmt.Fprint(slash.streams.Out, help)
	return err
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

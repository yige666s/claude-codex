package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	authapp "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	lspcore "claude-codex/internal/app/lsp"
	"claude-codex/internal/app/securestorage"
	bridgepkg "claude-codex/internal/backend/bridge"
	anthropicbackend "claude-codex/internal/harness/anthropic"
	api "claude-codex/internal/harness/anthropic"
	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/engine"
	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/plugins"
	providerbackend "claude-codex/internal/harness/provider"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	coretasks "claude-codex/internal/harness/tasks"
	"claude-codex/internal/harness/telemetry"
	toolkit "claude-codex/internal/harness/tools"
	agenttool "claude-codex/internal/harness/tools/agent"
	askquestiontool "claude-codex/internal/harness/tools/askuserquestion"
	bashtool "claude-codex/internal/harness/tools/bash"
	brieftool "claude-codex/internal/harness/tools/brief"
	configtool "claude-codex/internal/harness/tools/configtool"
	crontool "claude-codex/internal/harness/tools/cron"
	filetool "claude-codex/internal/harness/tools/file"
	lsptool "claude-codex/internal/harness/tools/lsp"
	mcptool "claude-codex/internal/harness/tools/mcp"
	mcpresourcestool "claude-codex/internal/harness/tools/mcpresources"
	notebooktool "claude-codex/internal/harness/tools/notebook"
	planmodetool "claude-codex/internal/harness/tools/planmode"
	powershelltool "claude-codex/internal/harness/tools/powershell"
	remotetriggertool "claude-codex/internal/harness/tools/remotetrigger"
	repltool "claude-codex/internal/harness/tools/repl"
	searchtool "claude-codex/internal/harness/tools/search"
	sendmessagetool "claude-codex/internal/harness/tools/sendmessage"
	skilltool "claude-codex/internal/harness/tools/skill"
	sleeptool "claude-codex/internal/harness/tools/sleep"
	synthetictool "claude-codex/internal/harness/tools/synthetic"
	tasktool "claude-codex/internal/harness/tools/tasks"
	teamtool "claude-codex/internal/harness/tools/team"
	testingpermissiontool "claude-codex/internal/harness/tools/testingpermission"
	todotool "claude-codex/internal/harness/tools/todo"
	toolsearchtool "claude-codex/internal/harness/tools/toolsearch"
	webtool "claude-codex/internal/harness/tools/web"
	worktreetool "claude-codex/internal/harness/tools/worktree"
	"claude-codex/internal/public/apperrors"
	"claude-codex/internal/ui/tui"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type bridgeRunner struct {
	defaultWorkDir string
	home           string
	buildEngine    func(string) (*engine.Engine, error)
}

var startTUI = func(options tui.Options) error {
	return tui.Run(options)
}

func NewRootCommand() *cobra.Command {
	return NewRootCommandWithIO(IO{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	})
}

func NewRootCommandWithIO(streams IO) *cobra.Command {
	if streams.In == nil {
		streams.In = os.Stdin
	}
	if streams.Out == nil {
		streams.Out = os.Stdout
	}
	if streams.Err == nil {
		streams.Err = os.Stderr
	}

	var (
		backendFlag        string
		modelFlag          string
		permissionModeFlag string
		cwdFlag            string
		saveSessionFlag    bool
		maxTurnsFlag       int
	)

	command := &cobra.Command{
		Use:          "claude [prompt]",
		Short:        "Claude Codex CLI",
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if backendFlag != "" {
				cfg.Backend = backendFlag
			}
			if modelFlag != "" {
				cfg.Model = modelFlag
			}
			if permissionModeFlag != "" {
				cfg.PermissionMode = permissionModeFlag
			}
			if cmd.Flags().Changed("max-turns") {
				cfg.MaxTurns = maxTurnsFlag
			}

			securestorage.StartPrefetchForConfig(cfg)

			mode, err := permissions.ParseMode(cfg.PermissionMode)
			if err != nil {
				return err
			}

			if cwdFlag == "" {
				cwdFlag, err = os.Getwd()
				if err != nil {
					return err
				}
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			home, err := config.AppHome()
			if err != nil {
				return err
			}
			if err := coretasks.DefaultManager().LoadRuntimeSnapshot(home, cwdFlag); err != nil {
				fmt.Fprintf(streams.Err, "warning: could not restore task snapshot: %v\n", err)
			}
			defer func() {
				if err := coretasks.DefaultManager().SaveRuntimeSnapshot(home, cwdFlag); err != nil {
					fmt.Fprintf(streams.Err, "warning: could not save task snapshot: %v\n", err)
				}
			}()
			telemetryRuntime, err := newTelemetryRuntime(cfg, home, streams)
			if err != nil {
				return err
			}
			defer func() {
				_ = telemetryRuntime.Close()
			}()
			var loadedPlugins []*plugins.LoadedPlugin
			if strings.TrimSpace(cfg.PluginDir) != "" {
				loadedPlugins, err = plugins.NewLoader(cfg.PluginDir).LoadDetailed(plugins.LoadOptions{
					Marketplace: InlinePluginMarketplaceForCLI(),
					Repository:  "plugin_dir",
				})
				if err != nil {
					return err
				}
				if telemetryRuntime.IsEnabled() {
					for _, plugin := range loadedPlugins {
						_ = telemetryRuntime.Tracer().Record(telemetry.BuildPluginEvent(plugins.Manifest{
							Name:       plugin.Source,
							Version:    plugin.Manifest.Version,
							Path:       plugin.Path,
							MCPServers: plugin.MCPServers,
						}, nil))
					}
				}
				cfg.MCPServers = append(cfg.MCPServers, plugins.MCPServerConfigs(loadedPlugins)...)
			}
			skillManager := loadSkillManager(cwdFlag, streams, loadedPlugins)
			baseRuntime := newRuntimeServices(cwdFlag, home, nil, nil)
			baseRuntime.warmupMagicDocs()
			unregisterReadListener := baseRuntime.registerFileReadListener()
			defer unregisterReadListener()

			var buildEngine func(string) (*engine.Engine, error)
			buildEngine = func(workingDir string) (*engine.Engine, error) {
				eng, err := newEngine(
					cfg,
					mode,
					workingDir,
					streams,
					makeSubagentRunner(cfg, mode, streams, skillManager, nil, nil, telemetryRuntime.Tracer()),
					telemetryRuntime.Tracer(),
				)
				if err != nil {
					return nil, err
				}
				attachCoordinatorTaskNotifications(eng)
				return eng, nil
			}

			if isMCPServerModeEnabled() {
				registry, err := buildRegistry(cfg, cwdFlag, nil, nil)
				if err != nil {
					return err
				}
				server := mcpcore.NewServer(registry)
				return server.ServeStdio(cmd.Context(), streams.In, streams.Out)
			}

			if isBridgeModeEnabled() {
				runner := bridgeRunner{
					defaultWorkDir: cwdFlag,
					home:           home,
					buildEngine:    buildEngine,
				}
				stopBridgeContinuation := startBridgeContinuationForwarder(cmd.Context(), runner, coretasks.DefaultManager())
				defer stopBridgeContinuation()
				server := bridgepkg.NewServer(cfg.BridgeSecret, runner)
				return server.Serve(cmd.Context(), streams.In, streams.Out)
			}

			if len(args) == 0 {
				return runInteractive(cmd.Context(), cfg, mode, cwdFlag, home, saveSessionFlag, streams, buildEngine, telemetryRuntime.Tracer(), skillManager)
			}

			prompt := strings.Join(args, " ")
			if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
				return runSlashCommand(cmd.Context(), prompt, slashContext{
					cfg:             &cfg,
					home:            home,
					defaultWorkDir:  cwdFlag,
					streams:         streams,
					saveSession:     saveSessionFlag,
					newEngineForDir: buildEngine,
					skillManager:    skillManager,
				})
			}

			runner, err := buildEngine(cwdFlag)
			if err != nil {
				return err
			}

			session := state.NewSession(cwdFlag)
			result, err := runner.Run(cmd.Context(), session, prompt)
			if err != nil {
				return err
			}

			if strings.TrimSpace(result.Output) != "" {
				if _, err := fmt.Fprintln(streams.Out, result.Output); err != nil {
					return err
				}
			}

			if saveSessionFlag {
				if _, err := session.Save(home); err != nil {
					return err
				}
				baseRuntime.maybeRunAutoDream()
			}

			return nil
		},
	}

	command.Flags().StringVar(&backendFlag, "backend", "", "planner backend: simple, anthropic, or openai")
	command.Flags().StringVar(&modelFlag, "model", "", "model name for remote backend use")
	command.Flags().StringVar(&permissionModeFlag, "permission-mode", "", "permission mode: default, plan, bypass, auto")
	command.Flags().StringVar(&cwdFlag, "cwd", "", "project root for file and shell tools")
	command.Flags().BoolVar(&saveSessionFlag, "save-session", true, "persist the session transcript under CLAUDE_GO_HOME")
	command.Flags().IntVar(&maxTurnsFlag, "max-turns", 0, "maximum number of agentic turns (0 = use config default, unlimited by default)")

	return command
}

func buildSubagentPrompt(request agenttool.Request) string {
	var preamble []string
	if request.Description != "" {
		preamble = append(preamble, "Task summary: "+request.Description)
	}
	if request.SubagentType != "" {
		preamble = append(preamble, "Requested subagent type: "+request.SubagentType)
	}
	if request.DefinitionSource != "" {
		preamble = append(preamble, "Agent definition source: "+request.DefinitionSource)
	}
	if request.DefinitionMemory != "" {
		preamble = append(preamble, "Agent memory policy: "+request.DefinitionMemory)
	}
	if request.PermissionPolicy != "" {
		preamble = append(preamble, "Agent permission policy: "+request.PermissionPolicy)
	}
	if len(request.DefinitionSkills) > 0 {
		preamble = append(preamble, "Agent requested skills: "+strings.Join(request.DefinitionSkills, ", "))
	}
	if len(request.DefinitionMCPServers) > 0 {
		preamble = append(preamble, "Agent MCP servers: "+strings.Join(request.DefinitionMCPServers, ", "))
	}
	if len(request.DefinitionRequiredMCPServers) > 0 {
		preamble = append(preamble, "Agent required MCP servers: "+strings.Join(request.DefinitionRequiredMCPServers, ", ")+". If a required MCP server is unavailable in this runtime, report that limitation explicitly.")
	}
	if request.OmitClaudeMd {
		preamble = append(preamble, "Project CLAUDE.md context is intentionally omitted for this agent definition.")
	}
	if request.ParentSessionID != "" {
		preamble = append(preamble, "Parent session ID: "+request.ParentSessionID)
	}
	if request.ParentAgentID != "" {
		preamble = append(preamble, "Parent agent ID: "+request.ParentAgentID)
	}
	if request.Isolation != "" {
		preamble = append(preamble, "Agent isolation: "+request.Isolation)
	}
	preamble = append(preamble, request.Prompt)
	return strings.Join(preamble, "\n\n")
}

func newEngine(cfg config.Config, mode permissions.Mode, workingDir string, streams IO, runSubagent agenttool.Runner, tracer telemetry.SessionTracer) (*engine.Engine, error) {
	return newEngineWithOptions(cfg, mode, workingDir, streams, runSubagent, tracer, nil)
}

func attachCoordinatorTaskNotifications(eng *engine.Engine) {
	if eng == nil || !coordinator.IsCoordinatorMode() {
		return
	}
	eng.AddPendingMessageProvider(func(context.Context) []string {
		return coordinator.DrainTaskNotifications(coretasks.DefaultManager())
	})
}

func buildRegistry(cfg config.Config, workingDir string, runSubagent agenttool.Runner, skillManager *skills.SkillManager) (*toolkit.Registry, error) {
	authManager, err := authapp.NewManager(cfg, nil)
	if err != nil {
		return nil, err
	}
	var briefUploader brieftool.Uploader
	if brieftool.UploadEnabledFromEnv() {
		briefUploader = brieftool.NewOAuthUploader(authManager, nil)
	}
	worktreeManager := coordinator.NewWorktreeManager(workingDir)
	tools := []toolkit.Tool{
		askquestiontool.NewTool(),
		bashtool.NewTool(workingDir),
		brieftool.NewToolWithUploader(workingDir, briefUploader),
		brieftool.NewFileToolWithUploader(workingDir, briefUploader),
		brieftool.NewLegacyToolWithUploader(workingDir, briefUploader),
		configtool.NewTool(workingDir),
		crontool.NewCronCreate(),
		crontool.NewCronDelete(),
		crontool.NewCronList(),
		planmodetool.NewEnterTool(),
		planmodetool.NewExitTool(),
		filetool.NewReadTool(workingDir),
		filetool.NewWriteTool(workingDir),
		filetool.NewEditTool(workingDir),
		notebooktool.NewEditTool(workingDir),
		powershelltool.NewTool(workingDir),
		searchtool.NewGlobTool(workingDir),
		searchtool.NewGrepTool(workingDir),
		webtool.NewSearchTool(nil),
		webtool.NewFetchTool(nil),
		lsptool.NewTool(workingDir, lspcore.NewLocalManager(workingDir)),
		teamtool.NewTeamCreateTool(coordinator.NewManager(coordinator.Config{ScratchpadDir: workingDir})),
		teamtool.NewTeamDeleteTool(coordinator.NewManager(coordinator.Config{ScratchpadDir: workingDir})),
		worktreetool.NewEnterTool(worktreeManager),
		worktreetool.NewExitTool(worktreeManager),
		tasktool.NewTaskCreateTool(),
		tasktool.NewTaskGetTool(),
		tasktool.NewTaskListTool(),
		tasktool.NewTaskUpdateTool(),
		tasktool.NewTaskStopTool(),
		tasktool.NewTaskOutputTool(),
		todotool.New(),
		&sendmessagetool.Tool{},
		remotetriggertool.NewTool(authManager),
		mcpresourcestool.NewListMcpResources(""),
		mcpresourcestool.NewReadMcpResource(),
	}
	if testingpermissiontool.EnabledFromEnv() {
		tools = append(tools, testingpermissiontool.NewTool())
	}
	if optionalToolEnabled("CLAUDE_GO_ENABLE_SLEEP_TOOL", "PROACTIVE", "KAIROS") {
		tools = append(tools, sleeptool.New())
	}
	if optionalToolEnabled("CLAUDE_GO_ENABLE_SYNTHETIC_OUTPUT_TOOL", "CLAUDE_CODE_SYNTHETIC_OUTPUT") {
		tools = append(tools, synthetictool.New())
	}
	if runSubagent != nil {
		agentTool := agenttool.NewTool(workingDir, runSubagent)
		agentTool.SetAvailableMCPServers(mcpServerNames(cfg.MCPServers))
		tools = append(tools, agentTool)
	}
	if skillManager != nil {
		skilltool := skilltool.NewToolWithRunner(skillManager, workingDir, runSubagent)
		tools = append(tools, skilltool)
	}
	if _, err := plugins.NewLoader(cfg.PluginDir).Load(); err != nil {
		return nil, err
	}
	if len(cfg.MCPServers) > 0 {
		discoveryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		clients, definitions, err := mcpcore.DiscoverTools(discoveryCtx, cfg.MCPServers, nil)
		if err != nil {
			return nil, err
		}
		for serverName, defs := range definitions {
			client := clients[serverName]
			for _, definition := range defs {
				tools = append(tools, mcptool.NewRemoteTool(serverName, definition, client))
			}
		}
	}
	tools = append(tools, toolsearchtool.New(toolNames(tools)))
	if repltool.ModeEnabledFromEnv() {
		tools = append(tools, repltool.NewTool(tools))
		tools = repltool.FilterToolsForMode(tools)
	}
	return toolkit.NewRegistry(tools...), nil
}

func toolNames(tools []toolkit.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name())
	}
	return names
}

func mcpServerNames(servers []config.MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		name := strings.TrimSpace(server.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func optionalToolEnabled(envNames ...string) bool {
	for _, name := range envNames {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "1", "true", "t", "yes", "y", "on":
			return true
		}
	}
	return false
}

func newEngineWithOptions(
	cfg config.Config,
	mode permissions.Mode,
	workingDir string,
	streams IO,
	runSubagent agenttool.Runner,
	tracer telemetry.SessionTracer,
	skillManager *skills.SkillManager,
	checkerOptions ...permissions.Option,
) (*engine.Engine, error) {
	planner, err := newPlanner(cfg)
	if err != nil {
		return nil, err
	}
	registry, err := buildRegistry(cfg, workingDir, runSubagent, skillManager)
	if err != nil {
		return nil, err
	}
	runtimePermissionOptions, err := newPermissionRuntimeOptions(cfg, mode, workingDir)
	if err != nil {
		return nil, err
	}
	checkerOptions = append(runtimePermissionOptions, checkerOptions...)

	eng := engine.NewWithDir(
		planner,
		registry,
		permissions.NewChecker(mode, streams.In, streams.Err, checkerOptions...),
		cfg.MaxTurns,
		workingDir,
	)
	eng.SetSkillManager(skillManager)
	eng.SetTelemetryTracer(tracer)
	return eng, nil
}

func runInteractive(
	ctx context.Context,
	cfg config.Config,
	mode permissions.Mode,
	workingDir string,
	home string,
	saveSession bool,
	streams IO,
	buildEngine func(string) (*engine.Engine, error),
	tracer telemetry.SessionTracer,
	skillManager *skills.SkillManager,
) error {
	broker := tui.NewPermissionBroker()
	currentConfig := cfg
	session := state.NewSession(workingDir)

	// Create progress channel for tool execution feedback
	progressCh := make(chan toolkit.ProgressEvent, 100)
	promptSuggestionCh := make(chan string, 8)
	coordinatorEventCh := make(chan string, 16)
	stopCoordinatorForwarder := coordinator.ForwardTaskNotifications(coretasks.DefaultManager(), coordinatorEventCh)
	defer stopCoordinatorForwarder()
	runtime := newRuntimeServices(workingDir, home, promptSuggestionCh, func(event toolkit.ProgressEvent) {
		select {
		case progressCh <- event:
		default:
		}
	})
	runtime.warmupMagicDocs()
	unregisterReadListener := runtime.registerFileReadListener()
	defer unregisterReadListener()

	// Log skill stats
	stats := skillManager.GetStats()
	if stats.TotalSkills > 0 {
		fmt.Fprintf(streams.Err, "Loaded %d skills (%d bundled, %d custom, %d user-invocable)\n",
			stats.TotalSkills, stats.BundledSkills, stats.DynamicSkills, stats.UserInvocable)
	}

	// Build a streaming runner when the backend supports it
	var streamRunner tui.StreamRunner
	if cfg.Backend == "anthropic" {
		if sr, err := newStreamRunner(cfg); err == nil {
			streamRunner = func(runCtx context.Context, currentSession *state.Session, prompt string, onChunk func(string)) (engine.Result, error) {
				// Build engine so we can get tool descriptors and execute tools if needed
				eng, err := newEngineWithOptions(
					currentConfig,
					mode,
					currentSession.WorkingDir,
					streams,
					makeSubagentRunner(
						currentConfig,
						mode,
						streams,
						skillManager,
						func(requestCtx context.Context, request permissions.Request) error {
							return broker.Authorize(requestCtx, request)
						},
						func(event toolkit.ProgressEvent) {
							select {
							case progressCh <- event:
							default:
							}
						},
						tracer,
					),
					tracer,
					skillManager,
					permissions.WithDecisionHandler(func(requestCtx context.Context, request permissions.Request) (permissions.Decision, error) {
						return broker.AuthorizeDecision(requestCtx, request)
					}),
				)
				if err != nil {
					return engine.Result{}, err
				}
				attachCoordinatorTaskNotifications(eng)

				// Set progress callback to send events to TUI
				eng.SetProgressCallback(func(event toolkit.ProgressEvent) {
					select {
					case progressCh <- event:
					default:
						// Channel full, skip this event
					}
				})

				plan, err := sr.StreamNext(runCtx, currentSession, eng.Descriptors(), onChunk)
				if err != nil {
					recordStreamingInteraction(tracer, currentSession, prompt, "", err)
					return engine.Result{}, err
				}

				if len(plan.ToolCalls) > 0 {
					// Model wants to use tools — hand off to engine to execute the full loop.
					// The user message was already added by the TUI, so engine.Run will skip it.
					return eng.Run(runCtx, currentSession, prompt)
				}

				// Pure text response
				currentSession.AddAssistantMessage(plan.AssistantText)
				recordStreamingInteraction(tracer, currentSession, prompt, plan.AssistantText, nil)
				if saveSession {
					if _, err := currentSession.Save(home); err != nil {
						return engine.Result{}, err
					}
					runtime.maybeRunAutoDream()
				}
				runtime.updatePromptSuggestion(currentSession)
				return engine.Result{Output: plan.AssistantText, Session: currentSession}, nil
			}
		}
	}

	return startTUI(tui.Options{
		Title:            "Claude Codex",
		WorkingDir:       workingDir,
		Theme:            currentConfig.Theme,
		Session:          session,
		PermissionBroker: broker,
		PermissionMode:   string(mode),
		AuthStatus:       currentAuthStatus(currentConfig),
		LoadSandboxView: func() tui.SandboxViewData {
			return loadSandboxViewData(workingDir, string(mode))
		},
		LoadMCPView: func() tui.MCPViewData {
			return loadMCPViewData()
		},
		LoadTeamsView: func() tui.TeamsViewData {
			return loadTeamsViewData(workingDir)
		},
		SkillStats:         loadSkillStatsViewData(skillManager),
		ContextBudget:      loadContextBudgetViewData(currentConfig.Model),
		StreamRunner:       streamRunner,
		ProgressCh:         progressCh,
		PromptSuggestionCh: promptSuggestionCh,
		CoordinatorEventCh: coordinatorEventCh,
		Runner: func(runCtx context.Context, currentSession *state.Session, prompt string) (engine.Result, error) {
			runner, err := newEngineWithOptions(
				currentConfig,
				mode,
				currentSession.WorkingDir,
				streams,
				makeSubagentRunner(
					currentConfig,
					mode,
					streams,
					skillManager,
					func(requestCtx context.Context, request permissions.Request) error {
						return broker.Authorize(requestCtx, request)
					},
					func(event toolkit.ProgressEvent) {
						select {
						case progressCh <- event:
						default:
						}
					},
					tracer,
				),
				tracer,
				skillManager,
				permissions.WithDecisionHandler(func(requestCtx context.Context, request permissions.Request) (permissions.Decision, error) {
					return broker.AuthorizeDecision(requestCtx, request)
				}),
			)
			if err != nil {
				return engine.Result{}, err
			}
			attachCoordinatorTaskNotifications(runner)

			// Set progress callback to send events to TUI
			runner.SetProgressCallback(func(event toolkit.ProgressEvent) {
				select {
				case progressCh <- event:
				default:
					// Channel full, skip this event
				}
			})

			result, err := runner.Run(runCtx, currentSession, prompt)
			if err != nil {
				return engine.Result{}, err
			}
			if saveSession {
				if _, err := currentSession.Save(home); err != nil {
					return engine.Result{}, err
				}
				runtime.maybeRunAutoDream()
			}
			runtime.updatePromptSuggestion(currentSession)
			return result, nil
		},
		GeneratedRunner: func(runCtx context.Context, currentSession *state.Session, prompt string) (engine.Result, error) {
			runner, err := newEngineWithOptions(
				currentConfig,
				mode,
				currentSession.WorkingDir,
				streams,
				makeSubagentRunner(
					currentConfig,
					mode,
					streams,
					skillManager,
					func(requestCtx context.Context, request permissions.Request) error {
						return broker.Authorize(requestCtx, request)
					},
					func(event toolkit.ProgressEvent) {
						select {
						case progressCh <- event:
						default:
						}
					},
					tracer,
				),
				tracer,
				skillManager,
				permissions.WithDecisionHandler(func(requestCtx context.Context, request permissions.Request) (permissions.Decision, error) {
					return broker.AuthorizeDecision(requestCtx, request)
				}),
			)
			if err != nil {
				return engine.Result{}, err
			}
			attachCoordinatorTaskNotifications(runner)
			runner.SetProgressCallback(func(event toolkit.ProgressEvent) {
				select {
				case progressCh <- event:
				default:
				}
			})
			result, err := runner.RunGeneratedPrompt(runCtx, currentSession, prompt)
			if err != nil {
				return engine.Result{}, err
			}
			if saveSession {
				if _, err := currentSession.Save(home); err != nil {
					return engine.Result{}, err
				}
				runtime.maybeRunAutoDream()
			}
			runtime.updatePromptSuggestion(currentSession)
			return result, nil
		},
		SaveTheme: func(theme string) error {
			currentConfig.Theme = theme
			return config.Save(currentConfig)
		},
		Input:   streams.In,
		Output:  streams.Out,
		Err:     streams.Err,
		Context: ctx,
		Registry: NewCombinedRegistryAdapter(commandRegistry, skillManager, workingDir, slashContext{
			cfg:             &cfg,
			home:            home,
			defaultWorkDir:  workingDir,
			streams:         streams,
			saveSession:     saveSession,
			newEngineForDir: buildEngine,
			skillManager:    skillManager,
		}),
	})
}

func InlinePluginMarketplaceForCLI() string {
	return plugins.InlineMarketplaceName
}

func loadSkillManager(workingDir string, streams IO, loadedPlugins ...[]*plugins.LoadedPlugin) *skills.SkillManager {
	skillManager := skills.NewSkillManager()

	if err := skillManager.LoadBundledSkills(); err != nil && streams.Err != nil {
		fmt.Fprintf(streams.Err, "Warning: failed to load bundled skills: %v\n", err)
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		userSkillsDir := filepath.Join(homeDir, ".claude", "skills")
		_ = skillManager.LoadSkillsFromDirectory(userSkillsDir, skills.SourceFile)
		userCommandsDir := filepath.Join(homeDir, ".claude", "commands")
		_ = skillManager.LoadCommandsFromDirectory(userCommandsDir, skills.SourceFile)
	}

	projectSkillsDir := filepath.Join(workingDir, ".claude", "skills")
	_ = skillManager.LoadSkillsFromDirectory(projectSkillsDir, skills.SourceFile)
	projectCommandsDir := filepath.Join(workingDir, ".claude", "commands")
	_ = skillManager.LoadCommandsFromDirectory(projectCommandsDir, skills.SourceFile)

	for _, pluginList := range loadedPlugins {
		if len(pluginList) == 0 {
			continue
		}
		report, err := plugins.LoadRuntimeComponents(plugins.RuntimeOptions{
			Plugins:        pluginList,
			SkillManager:   skillManager,
			RegisterAgents: true,
		})
		if err != nil && streams.Err != nil {
			fmt.Fprintf(streams.Err, "Warning: failed to load plugin components: %v\n", err)
			continue
		}
		for _, warning := range report.Warnings {
			if streams.Err != nil {
				fmt.Fprintf(streams.Err, "Warning: %s\n", warning)
			}
		}
	}

	return skillManager
}

func (r bridgeRunner) RunPrompt(ctx context.Context, workingDir, prompt string) (string, error) {
	targetDir := r.defaultWorkDir
	if strings.TrimSpace(workingDir) != "" {
		targetDir = workingDir
	}
	runner, err := r.buildEngine(targetDir)
	if err != nil {
		return "", err
	}
	session := state.NewSession(targetDir)
	result, err := runner.Run(ctx, session, prompt)
	if err != nil {
		return "", err
	}
	if _, err := session.Save(r.home); err != nil {
		return "", err
	}
	return result.Output, nil
}

func (r bridgeRunner) ListTools(_ context.Context, workingDir string) ([]toolkit.Descriptor, error) {
	targetDir := r.defaultWorkDir
	if strings.TrimSpace(workingDir) != "" {
		targetDir = workingDir
	}
	runner, err := r.buildEngine(targetDir)
	if err != nil {
		return nil, err
	}
	return runner.Descriptors(), nil
}

func (r bridgeRunner) CreateSession(_ context.Context, workingDir string) (*bridgepkg.SessionInfo, error) {
	targetDir := r.defaultWorkDir
	if strings.TrimSpace(workingDir) != "" {
		targetDir = workingDir
	}
	session := state.NewSession(targetDir)
	if _, err := session.Save(r.home); err != nil {
		return nil, err
	}
	info := summarizeBridgeSession(session)
	return &info, nil
}

func (r bridgeRunner) RunSessionPrompt(ctx context.Context, sessionID, prompt string) (*bridgepkg.SessionPromptResult, error) {
	unlock := lockBridgeSession(sessionID)
	defer unlock()

	session, err := state.LoadSession(r.home, sessionID)
	if err != nil {
		return nil, err
	}
	workDir := session.WorkingDir
	if strings.TrimSpace(workDir) == "" {
		workDir = r.defaultWorkDir
	}
	runner, err := r.buildEngine(workDir)
	if err != nil {
		return nil, err
	}
	result, err := runner.Run(ctx, session, prompt)
	if err != nil {
		return nil, err
	}
	if _, err := session.Save(r.home); err != nil {
		return nil, err
	}
	return &bridgepkg.SessionPromptResult{
		Output:  result.Output,
		Session: summarizeBridgeSession(session),
	}, nil
}

func (r bridgeRunner) GetSession(_ context.Context, sessionID string) (*bridgepkg.SessionInfo, error) {
	session, err := state.LoadSession(r.home, sessionID)
	if err != nil {
		return nil, err
	}
	info := summarizeBridgeSession(session)
	return &info, nil
}

func (r bridgeRunner) ListSessions(_ context.Context, workingDir string) ([]bridgepkg.SessionInfo, error) {
	manager := state.NewSessionManager(r.home)
	sessions, err := manager.ListSessions(state.SearchOptions{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	result := make([]bridgepkg.SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		if strings.TrimSpace(workingDir) != "" && session.WorkingDir != workingDir {
			continue
		}
		result = append(result, summarizeBridgeSession(session))
	}
	return result, nil
}

func (r bridgeRunner) DeleteSession(_ context.Context, sessionID string) error {
	return state.NewSessionManager(r.home).DeleteSession(sessionID)
}

func summarizeBridgeSession(session *state.Session) bridgepkg.SessionInfo {
	if session == nil {
		return bridgepkg.SessionInfo{}
	}
	return bridgepkg.SessionInfo{
		ID:              session.ID,
		WorkingDir:      session.WorkingDir,
		StartedAt:       session.StartedAt,
		UpdatedAt:       session.UpdatedAt,
		MessageCount:    len(session.Messages),
		LastUserMessage: session.LastUserMessage(),
		Archived:        session.Archived,
	}
}

func isBridgeModeEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CLAUDE_BRIDGE_MODE"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "stdio")
}

func isMCPServerModeEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CLAUDE_MCP_SERVER_MODE"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "stdio")
}

func newPlanner(cfg config.Config) (engine.Planner, error) {
	switch cfg.Backend {
	case "", "simple":
		return engine.NewSimplePlanner(), nil
	case "anthropic":
		// Priority: config api_key > config api_token > ANTHROPIC_API_KEY env var
		apiKey := cfg.APIKey
		if config.IsPlaceholderAPIKey(apiKey) {
			apiKey = ""
		}
		if apiKey == "" {
			apiKey = cfg.APIToken
		}
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, apperrors.Auth(
				"API key is required for the anthropic backend.",
				"Set api_key in ~/.claude-codex/config.json, or export ANTHROPIC_API_KEY.",
				nil,
			)
		}
		timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
		client := api.NewClient(apiKey, cfg.APIBaseURL, timeout)
		return anthropicbackend.NewPlanner(client, cfg.Model), nil
	case "openai":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = cfg.APIToken
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, apperrors.Auth(
				"API key is required for the openai backend.",
				"Set api_key in ~/.claude-codex/config.json, or export OPENAI_API_KEY.",
				nil,
			)
		}
		baseURL := cfg.APIBaseURL
		if strings.TrimSpace(baseURL) == "" || strings.Contains(baseURL, "api.anthropic.com") {
			baseURL = ""
		}
		provider, err := providerbackend.NewOpenAIProvider(providerbackend.Config{
			Provider: "openai",
			APIKey:   apiKey,
			BaseURL:  baseURL,
			Model:    cfg.Model,
			Timeout:  cfg.TimeoutSeconds,
		})
		if err != nil {
			return nil, err
		}
		return providerbackend.NewPlanner(provider, cfg.Model), nil
	default:
		return nil, apperrors.Config(
			fmt.Sprintf("Unsupported backend %q.", cfg.Backend),
			"Use backend simple, anthropic, or openai.",
			nil,
		)
	}
}

// newStreamRunner creates an Anthropic streaming planner if the config supports it.
func newStreamRunner(cfg config.Config) (*anthropicbackend.Planner, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = cfg.APIToken
	}
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no api key")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	client := api.NewClient(apiKey, cfg.APIBaseURL, timeout)
	return anthropicbackend.NewPlanner(client, cfg.Model), nil
}

func currentAuthStatus(cfg config.Config) tui.AuthViewData {
	manager, err := authapp.NewManager(cfg, nil)
	if err != nil {
		return tui.AuthViewData{Status: "unavailable"}
	}
	status, err := manager.Status(context.Background())
	if err != nil || status == nil {
		return tui.AuthViewData{Status: "logged out"}
	}

	view := tui.AuthViewData{
		Authenticated:    status.Authenticated,
		HasTrustedDevice: status.HasTrustedDevice,
		Scopes:           append([]string(nil), status.Scopes...),
		SubscriptionType: string(status.SubscriptionType),
		RateLimitTier:    string(status.RateLimitTier),
	}
	if !status.ExpiresAt.IsZero() {
		view.ExpiresAt = status.ExpiresAt.Format(time.RFC3339)
	}
	if !status.Authenticated {
		view.Status = "logged out"
		return view
	}
	if status.HasTrustedDevice {
		view.Status = "authenticated + trusted device"
		return view
	}
	view.Status = "authenticated"
	return view
}

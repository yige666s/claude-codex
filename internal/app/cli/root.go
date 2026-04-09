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

	"github.com/ding/claude-code/claude-go/internal/app/config"
	lspcore "github.com/ding/claude-code/claude-go/internal/app/lsp"
	bridgepkg "github.com/ding/claude-code/claude-go/internal/backend/bridge"
	anthropicbackend "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
	"github.com/ding/claude-code/claude-go/internal/harness/coordinator"
	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	mcpcore "github.com/ding/claude-code/claude-go/internal/harness/mcp"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	"github.com/ding/claude-code/claude-go/internal/harness/plugins"
	"github.com/ding/claude-code/claude-go/internal/harness/skills"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
	agenttool "github.com/ding/claude-code/claude-go/internal/harness/tools/agent"
	bashtool "github.com/ding/claude-code/claude-go/internal/harness/tools/bash"
	filetool "github.com/ding/claude-code/claude-go/internal/harness/tools/file"
	lsptool "github.com/ding/claude-code/claude-go/internal/harness/tools/lsp"
	mcptool "github.com/ding/claude-code/claude-go/internal/harness/tools/mcp"
	notebooktool "github.com/ding/claude-code/claude-go/internal/harness/tools/notebook"
	searchtool "github.com/ding/claude-code/claude-go/internal/harness/tools/search"
	skilltool "github.com/ding/claude-code/claude-go/internal/harness/tools/skill"
	teamtool "github.com/ding/claude-code/claude-go/internal/harness/tools/team"
	webtool "github.com/ding/claude-code/claude-go/internal/harness/tools/web"
	worktreetool "github.com/ding/claude-code/claude-go/internal/harness/tools/worktree"
	"github.com/ding/claude-code/claude-go/internal/public/apperrors"
	"github.com/ding/claude-code/claude-go/internal/ui/tui"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type bridgeRunner struct {
	defaultWorkDir string
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
		Short:        "Claude Go CLI",
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
			if maxTurnsFlag > 0 {
				cfg.MaxTurns = maxTurnsFlag
			}

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

			var buildEngine func(string) (*engine.Engine, error)
			buildEngine = func(workingDir string) (*engine.Engine, error) {
				return newEngine(cfg, mode, workingDir, streams, func(ctx context.Context, req agenttool.Request) (string, error) {
					targetDir := workingDir
					if req.WorkingDir != "" {
						targetDir = req.WorkingDir
					}
					childEngine, err := newEngine(cfg, mode, targetDir, streams, nil)
					if err != nil {
						return "", err
					}
					childSession := state.NewSession(targetDir)
					result, err := childEngine.Run(ctx, childSession, req.Prompt)
					if err != nil {
						return "", err
					}
					return result.Output, nil
				})
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
				server := bridgepkg.NewServer(cfg.BridgeSecret, bridgeRunner{
					defaultWorkDir: cwdFlag,
					buildEngine:    buildEngine,
				})
				return server.Serve(cmd.Context(), streams.In, streams.Out)
			}

			if len(args) == 0 {
				return runInteractive(cmd.Context(), cfg, mode, cwdFlag, home, saveSessionFlag, streams, buildEngine)
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
			}

			return nil
		},
	}

	command.Flags().StringVar(&backendFlag, "backend", "", "planner backend: simple or anthropic")
	command.Flags().StringVar(&modelFlag, "model", "", "model name for remote backend use")
	command.Flags().StringVar(&permissionModeFlag, "permission-mode", "", "permission mode: default, plan, bypass, auto")
	command.Flags().StringVar(&cwdFlag, "cwd", "", "project root for file and shell tools")
	command.Flags().BoolVar(&saveSessionFlag, "save-session", true, "persist the session transcript under CLAUDE_GO_HOME")
	command.Flags().IntVar(&maxTurnsFlag, "max-turns", 0, "maximum number of agentic turns (0 = use config default)")

	return command
}

func newEngine(cfg config.Config, mode permissions.Mode, workingDir string, streams IO, runSubagent agenttool.Runner) (*engine.Engine, error) {
	return newEngineWithOptions(cfg, mode, workingDir, streams, runSubagent, nil)
}

func buildRegistry(cfg config.Config, workingDir string, runSubagent agenttool.Runner, skillManager *skills.SkillManager) (*toolkit.Registry, error) {
	tools := []toolkit.Tool{
		bashtool.NewTool(workingDir),
		filetool.NewReadTool(workingDir),
		filetool.NewWriteTool(workingDir),
		filetool.NewEditTool(workingDir),
		notebooktool.NewEditTool(workingDir),
		searchtool.NewGlobTool(workingDir),
		searchtool.NewGrepTool(workingDir),
		webtool.NewSearchTool(nil),
		webtool.NewFetchTool(nil),
		lsptool.NewTool(workingDir, lspcore.NewLocalManager(workingDir)),
		teamtool.NewTeamCreateTool(coordinator.NewManager(coordinator.Config{ScratchpadDir: workingDir})),
		teamtool.NewTeamDeleteTool(coordinator.NewManager(coordinator.Config{ScratchpadDir: workingDir})),
		worktreetool.NewEnterTool(coordinator.NewWorktreeManager(workingDir)),
	}
	if runSubagent != nil {
		tools = append(tools, agenttool.NewTool(workingDir, runSubagent))
	}
	if skillManager != nil {
		skilltool := skilltool.NewTool(skillManager)
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
	return toolkit.NewRegistry(tools...), nil
}

func newEngineWithOptions(
	cfg config.Config,
	mode permissions.Mode,
	workingDir string,
	streams IO,
	runSubagent agenttool.Runner,
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

	eng := engine.NewWithDir(
		planner,
		registry,
		permissions.NewChecker(mode, streams.In, streams.Err, checkerOptions...),
		cfg.MaxTurns,
		workingDir,
	)
	eng.SetSkillManager(skillManager)
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
) error {
	broker := tui.NewPermissionBroker()
	currentConfig := cfg
	session := state.NewSession(workingDir)

	// Create progress channel for tool execution feedback
	progressCh := make(chan toolkit.ProgressEvent, 100)

	// Initialize skill manager and load skills
	skillManager := skills.NewSkillManager()

	// Load bundled skills
	if err := skillManager.LoadBundledSkills(); err != nil {
		fmt.Fprintf(streams.Err, "Warning: failed to load bundled skills: %v\n", err)
	}

	// Load user skills from ~/.claude/skills
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userSkillsDir := filepath.Join(homeDir, ".claude", "skills")
		if err := skillManager.LoadSkillsFromDirectory(userSkillsDir, skills.SourceFile); err != nil {
			// Silent - user skills are optional
		}
	}

	// Load project skills from ./.claude/skills
	projectSkillsDir := filepath.Join(workingDir, ".claude", "skills")
	if err := skillManager.LoadSkillsFromDirectory(projectSkillsDir, skills.SourceFile); err != nil {
		// Silent - project skills are optional
	}

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
					nil,
					skillManager,
					permissions.WithRequestHandler(func(requestCtx context.Context, request permissions.Request) error {
						return broker.Authorize(requestCtx, request)
					}),
				)
				if err != nil {
					return engine.Result{}, err
				}

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
					return engine.Result{}, err
				}

				if len(plan.ToolCalls) > 0 {
					// Model wants to use tools — hand off to engine to execute the full loop.
					// The user message was already added by the TUI, so engine.Run will skip it.
					return eng.Run(runCtx, currentSession, prompt)
				}

				// Pure text response
				currentSession.AddAssistantMessage(plan.AssistantText)
				if saveSession {
					if _, err := currentSession.Save(home); err != nil {
						return engine.Result{}, err
					}
				}
				return engine.Result{Output: plan.AssistantText, Session: currentSession}, nil
			}
		}
	}

	return startTUI(tui.Options{
		Title:            "Claude Go",
		WorkingDir:       workingDir,
		Theme:            currentConfig.Theme,
		Session:          session,
		PermissionBroker: broker,
		StreamRunner:     streamRunner,
		ProgressCh:       progressCh,
		Runner: func(runCtx context.Context, currentSession *state.Session, prompt string) (engine.Result, error) {
			runner, err := newEngineWithOptions(
				currentConfig,
				mode,
				currentSession.WorkingDir,
				streams,
				nil,
				skillManager,
				permissions.WithRequestHandler(func(requestCtx context.Context, request permissions.Request) error {
					return broker.Authorize(requestCtx, request)
				}),
			)
			if err != nil {
				return engine.Result{}, err
			}

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
			}
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
		}),
	})
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
		if apiKey == "" {
			apiKey = cfg.APIToken
		}
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, apperrors.Auth(
				"API key is required for the anthropic backend.",
				"Set api_key in ~/.claude-go/config.json, or export ANTHROPIC_API_KEY.",
				nil,
			)
		}
		timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
		client := api.NewClient(apiKey, cfg.APIBaseURL, timeout)
		return anthropicbackend.NewPlanner(client, cfg.Model), nil
	default:
		return nil, apperrors.Config(
			fmt.Sprintf("Unsupported backend %q.", cfg.Backend),
			"Use backend simple or anthropic.",
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

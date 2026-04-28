package swarm

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// TeammateMode is the user-visible teammate runtime mode.
type TeammateMode string

const (
	TeammateModeAuto      TeammateMode = "auto"
	TeammateModeInProcess TeammateMode = "in-process"
	TeammateModeTmux      TeammateMode = "tmux"
	TeammateModeITerm2    TeammateMode = "iterm2"
)

// BackendEnvironment is the captured terminal/platform state used for backend selection.
type BackendEnvironment struct {
	InsideTmux     bool
	InITerm2       bool
	TmuxAvailable  bool
	ITermAvailable bool
	PreferTmux     bool
	NonInteractive bool
}

// DetectBackendEnvironment captures the same coarse signals as the TypeScript registry.
func DetectBackendEnvironment() BackendEnvironment {
	return BackendEnvironment{
		InsideTmux:     os.Getenv("TMUX") != "",
		InITerm2:       os.Getenv("TERM_PROGRAM") == "iTerm.app" || os.Getenv("ITERM_SESSION_ID") != "",
		TmuxAvailable:  commandAvailable("tmux"),
		ITermAvailable: commandAvailable("it2"),
		PreferTmux:     truthyEnv("CLAUDE_CODE_PREFER_TMUX_OVER_ITERM2"),
	}
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func truthyEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// BackendSelection requests a teammate executor mode. Auto mirrors TS resolution.
type BackendSelection struct {
	Mode TeammateMode
}

// BackendDetectionResult describes the selected executor and UI/setup hints.
type BackendDetectionResult struct {
	Executor        TeammateExecutor
	IsNative        bool
	NeedsITermSetup bool
	FallbackReason  string
}

// BackendRegistry resolves and caches teammate executors for the current process.
type BackendRegistry struct {
	env BackendEnvironment

	mu        sync.Mutex
	cached    *BackendDetectionResult
	fallback  bool
	executors map[BackendType]TeammateExecutor
}

// NewBackendRegistry creates a registry from a captured environment.
func NewBackendRegistry(env BackendEnvironment) *BackendRegistry {
	mailboxDir := defaultMailboxDir()
	return &BackendRegistry{
		env: env,
		executors: map[BackendType]TeammateExecutor{
			BackendTypeInProcess: NewInProcessBackend(nil, mailboxDir),
			BackendTypeTmux:      newPaneProcessBackend(BackendTypeTmux, env.TmuxAvailable, mailboxDir),
			BackendTypeITerm2:    newPaneProcessBackend(BackendTypeITerm2, env.ITermAvailable, mailboxDir),
		},
	}
}

func defaultMailboxDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return GetMailboxDir(home, "")
}

// NewDetectedBackendRegistry creates a registry from the live process environment.
func NewDetectedBackendRegistry() *BackendRegistry {
	return NewBackendRegistry(DetectBackendEnvironment())
}

// Select resolves a teammate executor.
func (r *BackendRegistry) Select(selection BackendSelection) (BackendDetectionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	mode := selection.Mode
	if mode == "" {
		mode = TeammateModeAuto
	}

	switch mode {
	case TeammateModeInProcess:
		return BackendDetectionResult{
			Executor:       r.mustExecutor(BackendTypeInProcess),
			FallbackReason: "explicit in-process mode",
		}, nil
	case TeammateModeTmux:
		return r.explicitPane(BackendTypeTmux)
	case TeammateModeITerm2:
		return r.explicitPane(BackendTypeITerm2)
	case TeammateModeAuto:
		if r.cached != nil {
			return *r.cached, nil
		}
		result := r.auto()
		r.cached = &result
		return result, nil
	default:
		return BackendDetectionResult{}, fmt.Errorf("unsupported teammate mode: %s", mode)
	}
}

func (r *BackendRegistry) explicitPane(typ BackendType) (BackendDetectionResult, error) {
	executor := r.mustExecutor(typ)
	if !executor.IsAvailable() {
		return BackendDetectionResult{}, fmt.Errorf("%s backend is not available", typ)
	}
	return BackendDetectionResult{Executor: executor, IsNative: r.isNative(typ)}, nil
}

func (r *BackendRegistry) auto() BackendDetectionResult {
	if r.env.NonInteractive {
		return r.inProcess("non-interactive sessions use in-process teammates")
	}
	if r.fallback {
		return r.inProcess("pane backend previously fell back to in-process")
	}
	if r.env.InsideTmux && r.env.TmuxAvailable {
		return BackendDetectionResult{Executor: r.mustExecutor(BackendTypeTmux), IsNative: true}
	}
	if r.env.InITerm2 && !r.env.PreferTmux {
		if r.env.ITermAvailable {
			return BackendDetectionResult{Executor: r.mustExecutor(BackendTypeITerm2), IsNative: true}
		}
		if r.env.TmuxAvailable {
			return BackendDetectionResult{
				Executor:        r.mustExecutor(BackendTypeTmux),
				IsNative:        false,
				NeedsITermSetup: true,
			}
		}
		r.fallback = true
		return r.inProcess("iTerm2 detected but neither it2 nor tmux is available")
	}
	if r.env.TmuxAvailable {
		return BackendDetectionResult{Executor: r.mustExecutor(BackendTypeTmux), IsNative: false}
	}
	r.fallback = true
	return r.inProcess("no pane backend is available")
}

func (r *BackendRegistry) inProcess(reason string) BackendDetectionResult {
	return BackendDetectionResult{
		Executor:       r.mustExecutor(BackendTypeInProcess),
		FallbackReason: reason,
	}
}

func (r *BackendRegistry) mustExecutor(typ BackendType) TeammateExecutor {
	if executor, ok := r.executors[typ]; ok {
		return executor
	}
	panic(fmt.Sprintf("missing executor for backend %s", typ))
}

func (r *BackendRegistry) isNative(typ BackendType) bool {
	return (typ == BackendTypeTmux && r.env.InsideTmux) || (typ == BackendTypeITerm2 && r.env.InITerm2)
}

type paneProcessBackend struct {
	typ        BackendType
	available  bool
	mailboxDir string
}

func newPaneProcessBackend(typ BackendType, available bool, mailboxDir string) *paneProcessBackend {
	return &paneProcessBackend{typ: typ, available: available, mailboxDir: mailboxDir}
}

func (b *paneProcessBackend) Type() BackendType { return b.typ }
func (b *paneProcessBackend) IsAvailable() bool { return b.available }

func (b *paneProcessBackend) Spawn(cfg TeammateSpawnConfig) (TeammateSpawnResult, error) {
	agentID := FormatAgentID(cfg.Name, cfg.TeamName)
	if !b.available {
		return TeammateSpawnResult{Success: false, AgentID: agentID, Error: fmt.Sprintf("%s backend is not available", b.typ)}, nil
	}
	command, err := buildPaneWorkerCommand(cfg, agentID)
	if err != nil {
		return TeammateSpawnResult{Success: false, AgentID: agentID, Error: err.Error()}, nil
	}
	paneID, err := b.launchPane(cfg, command)
	if err != nil {
		return TeammateSpawnResult{Success: false, AgentID: agentID, Error: err.Error()}, nil
	}
	member := TeamMember{
		AgentID:          string(agentID),
		Name:             cfg.Name,
		Color:            cfg.Color,
		PlanModeRequired: cfg.PlanModeRequired,
		JoinedAt:         time.Now().UnixMilli(),
		TmuxPaneID:       paneID,
		CWD:              cfg.CWD,
		WorktreePath:     cfg.WorktreePath,
		SessionID:        cfg.ParentSessionID,
		Subscriptions:    []string{},
		BackendType:      string(b.typ),
		IsActive:         true,
	}
	if cfg.TeamName != "" {
		_ = UpsertMember(cfg.TeamName, member)
	}
	return TeammateSpawnResult{
		Success: true,
		AgentID: agentID,
		TaskID:  paneID,
	}, nil
}

func (b *paneProcessBackend) launchPane(cfg TeammateSpawnConfig, command string) (string, error) {
	cwd := strings.TrimSpace(cfg.CWD)
	if cfg.WorktreePath != "" {
		cwd = cfg.WorktreePath
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	switch b.typ {
	case BackendTypeTmux:
		args := []string{"split-window", "-P", "-F", "#{pane_id}", "-c", cwd, command}
		out, err := exec.Command("tmux", args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("tmux pane launch failed: %s", strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	case BackendTypeITerm2:
		out, err := exec.Command("it2", "run", "--cwd", cwd, command).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("iTerm2 pane launch failed: %s", strings.TrimSpace(string(out)))
		}
		id := strings.TrimSpace(string(out))
		if id == "" {
			id = fmt.Sprintf("iterm2:%s", time.Now().Format("150405.000"))
		}
		return id, nil
	default:
		return "", fmt.Errorf("unsupported pane backend: %s", b.typ)
	}
}

func buildPaneWorkerCommand(cfg TeammateSpawnConfig, agentID AgentID) (string, error) {
	template := strings.TrimSpace(os.Getenv("CLAUDE_CODE_TEAM_WORKER_CMD"))
	if template == "" {
		template = strings.TrimSpace(os.Getenv("CODEX_TEAM_WORKER_CMD"))
	}
	if template == "" {
		return "", fmt.Errorf("worker command is not configured; set CLAUDE_CODE_TEAM_WORKER_CMD")
	}
	replacements := map[string]string{
		"{agent_id}":  string(agentID),
		"{name}":      cfg.Name,
		"{team_name}": cfg.TeamName,
		"{cwd}":       cfg.CWD,
		"{prompt}":    cfg.Prompt,
	}
	command := template
	for token, value := range replacements {
		command = strings.ReplaceAll(command, token, shellQuote(value))
	}
	return command, nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (b *paneProcessBackend) SendMessage(agentID AgentID, msg TeammateMessage) error {
	return WriteToMailbox(b.mailboxDir, string(agentID), MailboxEntry{
		From:      msg.From,
		Text:      msg.Text,
		Color:     msg.Color,
		Timestamp: msg.Timestamp,
	})
}

func (b *paneProcessBackend) Terminate(agentID AgentID, reason string) error {
	return WriteToMailbox(b.mailboxDir, string(agentID), MailboxEntry{
		From: TeamLeadName,
		Text: fmt.Sprintf(`{"type":"shutdown_request","reason":%q}`, reason),
	})
}

func (b *paneProcessBackend) Kill(agentID AgentID) error {
	if _, teamName := ParseAgentID(agentID); teamName != "" {
		_, _ = RemoveMemberByAgentID(teamName, string(agentID))
	}
	return nil
}

func (b *paneProcessBackend) IsActive(agentID AgentID) bool {
	if _, teamName := ParseAgentID(agentID); teamName != "" {
		member, err := FindMemberByAgentID(teamName, string(agentID))
		return err == nil && member != nil && member.IsActive
	}
	return false
}

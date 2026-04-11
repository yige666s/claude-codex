package cli

import (
	"os"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/app/config"
	contextwindow "claude-codex/internal/harness/context"
	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/swarm"
	"claude-codex/internal/ui/tui"
)

func loadSandboxViewData(workingDir, permissionMode string) tui.SandboxViewData {
	view := tui.SandboxViewData{
		Mode:          permissionMode,
		ExecutionEnv:  detectExecutionEnv(),
		WorkingDir:    workingDir,
		WritableRoots: []string{workingDir},
	}
	switch permissionMode {
	case "bypass":
		view.ApprovalPolicy = "write and execute actions run without prompts"
	case "plan":
		view.ApprovalPolicy = "write and execute actions are blocked"
	case "auto":
		view.ApprovalPolicy = "read-only actions are allowed; writes remain blocked"
	default:
		view.ApprovalPolicy = "write and execute actions require approval"
	}
	view.Notes = []string{
		"Go TUI currently exposes execution guards and environment hints, not the full TS sandbox settings editor.",
		"Use /config and permission mode flags for current runtime control.",
	}
	return view
}

func loadMCPViewData() tui.MCPViewData {
	cfg, err := config.Load()
	if err != nil {
		return tui.MCPViewData{}
	}
	view := tui.MCPViewData{
		Servers:  make([]tui.MCPServerViewData, 0, len(cfg.MCPServers)),
		LoadedAt: time.Now().Format(time.RFC3339),
	}
	for _, server := range cfg.MCPServers {
		target := server.URL
		if target == "" && len(server.Command) > 0 {
			target = strings.Join(server.Command, " ")
		}
		view.Servers = append(view.Servers, tui.MCPServerViewData{
			Name:      server.Name,
			Transport: fallbackTransport(server.Transport),
			Target:    target,
			Source:    "config",
		})
	}
	sort.Slice(view.Servers, func(i, j int) bool {
		return view.Servers[i].Name < view.Servers[j].Name
	})
	return view
}

func loadTeamsViewData(workingDir string) tui.TeamsViewData {
	view := tui.TeamsViewData{LoadedAt: time.Now().Format(time.RFC3339)}

	manager := coordinator.NewTeamManager(workingDir)
	if teams, err := manager.List(); err == nil {
		for _, team := range teams {
			view.Teams = append(view.Teams, tui.TeamViewData{
				Name:      team.Name,
				Source:    "coordinator",
				CreatedAt: team.CreatedAt.Format(time.RFC3339),
			})
		}
	}

	if swarmTeams, err := swarm.ListTeams(); err == nil {
		for _, teamName := range swarmTeams {
			tf, readErr := swarm.ReadTeamFile(teamName)
			if readErr != nil || tf == nil {
				continue
			}
			pendingCount := 0
			if pending, err := swarm.ReadPendingPermissions(teamName); err == nil {
				pendingCount = len(pending)
			}
			teamView := tui.TeamViewData{
				Name:               firstNonEmpty(tf.Name, teamName),
				Source:             "swarm",
				Description:        tf.Description,
				CreatedAt:          formatUnixMilli(tf.CreatedAt),
				PendingPermissions: pendingCount,
				Members:            make([]tui.TeamMemberViewData, 0, len(tf.Members)),
			}
			for _, member := range tf.Members {
				teamView.Members = append(teamView.Members, tui.TeamMemberViewData{
					Name:      member.Name,
					AgentID:   member.AgentID,
					AgentType: member.AgentType,
					Model:     member.Model,
					Mode:      member.Mode,
					Backend:   member.BackendType,
					CWD:       member.CWD,
					Active:    member.IsActive,
				})
			}
			sort.Slice(teamView.Members, func(i, j int) bool {
				return teamView.Members[i].Name < teamView.Members[j].Name
			})
			view.Teams = append(view.Teams, teamView)
		}
	}

	sort.Slice(view.Teams, func(i, j int) bool {
		if view.Teams[i].Name == view.Teams[j].Name {
			return view.Teams[i].Source < view.Teams[j].Source
		}
		return view.Teams[i].Name < view.Teams[j].Name
	})
	return view
}

func loadSkillStatsViewData(skillManager *skills.SkillManager) tui.SkillStatsViewData {
	if skillManager == nil {
		return tui.SkillStatsViewData{}
	}
	stats := skillManager.GetStats()
	return tui.SkillStatsViewData{
		Total:         stats.TotalSkills,
		UserInvocable: stats.UserInvocable,
		Dynamic:       stats.DynamicSkills,
		Conditional:   stats.ConditionalSkills,
	}
}

func loadContextBudgetViewData(model string) tui.ContextBudgetViewData {
	compression := state.DefaultCompressionConfig()
	return tui.ContextBudgetViewData{
		Model:                   strings.TrimSpace(model),
		ContextWindowTokens:     contextwindow.GetContextWindowForModel(model),
		CompressionSoftTokens:   compression.MaxTokens,
		CompressionTargetTokens: compression.TargetTokens,
	}
}

func detectExecutionEnv() string {
	if os.Getenv("IS_SANDBOX") != "" {
		return "sandbox"
	}
	if os.Getenv("container") != "" || os.Getenv("DOCKER_CONTAINER") != "" {
		return "docker"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	return "host"
}

func fallbackTransport(value string) string {
	if strings.TrimSpace(value) == "" {
		return "stdio"
	}
	return value
}

func formatUnixMilli(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

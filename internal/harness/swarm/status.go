package swarm

import "fmt"

// TeamStatusSnapshot is a compact view model for TUI/web team indicators.
type TeamStatusSnapshot struct {
	TeamName        string
	LeadAgentID     string
	TotalTeammates  int
	ActiveTeammates int
	BackendCounts   map[BackendType]int
	Members         []MemberStatusSnapshot
	FooterText      string
	NeedsAttention  bool
	TeamFilePath    string
	LeadSessionID   string
}

// MemberStatusSnapshot is the UI-ready status for one teammate.
type MemberStatusSnapshot struct {
	AgentID       string
	Name          string
	Color         string
	BackendType   BackendType
	PaneID        string
	SessionID     string
	Mode          string
	IsActive      bool
	DisplayStatus string
	CWD           string
	WorktreePath  string
}

// BuildTeamStatusSnapshot reads the team file and derives counts/status text.
func BuildTeamStatusSnapshot(teamName string) (*TeamStatusSnapshot, error) {
	tf, err := ReadTeamFile(teamName)
	if err != nil || tf == nil {
		return nil, err
	}

	snapshot := &TeamStatusSnapshot{
		TeamName:      tf.Name,
		LeadAgentID:   tf.LeadAgentID,
		LeadSessionID: tf.LeadSessionID,
		BackendCounts: make(map[BackendType]int),
		TeamFilePath:  GetTeamFilePath(teamName),
	}
	for _, member := range tf.Members {
		backend := BackendType(member.BackendType)
		if backend == "" {
			backend = backendTypeFromMember(member)
		}
		status := "disconnected"
		if member.IsActive {
			status = "running"
			snapshot.ActiveTeammates++
		} else {
			snapshot.NeedsAttention = true
		}
		snapshot.TotalTeammates++
		snapshot.BackendCounts[backend]++
		snapshot.Members = append(snapshot.Members, MemberStatusSnapshot{
			AgentID:       member.AgentID,
			Name:          member.Name,
			Color:         member.Color,
			BackendType:   backend,
			PaneID:        member.TmuxPaneID,
			SessionID:     member.SessionID,
			Mode:          member.Mode,
			IsActive:      member.IsActive,
			DisplayStatus: status,
			CWD:           member.CWD,
			WorktreePath:  member.WorktreePath,
		})
	}
	if snapshot.TotalTeammates == 0 {
		snapshot.FooterText = "0 teammates"
	} else if snapshot.ActiveTeammates == snapshot.TotalTeammates {
		snapshot.FooterText = fmt.Sprintf("%d teammates", snapshot.TotalTeammates)
	} else {
		snapshot.FooterText = fmt.Sprintf("%d/%d teammates", snapshot.ActiveTeammates, snapshot.TotalTeammates)
	}
	return snapshot, nil
}

func backendTypeFromMember(member TeamMember) BackendType {
	switch {
	case member.TmuxPaneID == InProcessMarker:
		return BackendTypeInProcess
	case member.TmuxPaneID != "":
		return BackendTypeTmux
	default:
		return BackendTypeInProcess
	}
}

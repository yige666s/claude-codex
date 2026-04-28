package swarm

// InitialTeamContextOptions are CLI/transcript-derived identity hints used at startup.
type InitialTeamContextOptions struct {
	TeamName  string
	AgentID   string
	AgentName string
}

// InitialTeamContext is the restored swarm context consumed by runtime/UI state.
type InitialTeamContext struct {
	TeamName         string
	TeamFilePath     string
	LeadAgentID      string
	SelfAgentID      string
	SelfAgentName    string
	IsLeader         bool
	BackendType      BackendType
	SessionID        string
	Mode             string
	Color            string
	PlanModeRequired bool
	Teammates        map[string]TeamMember
}

// ComputeInitialTeamContext restores teammate/leader context from the team file.
func ComputeInitialTeamContext(opts InitialTeamContextOptions) (*InitialTeamContext, error) {
	if opts.TeamName == "" || opts.AgentName == "" {
		return nil, nil
	}

	tf, err := ReadTeamFile(opts.TeamName)
	if err != nil || tf == nil {
		return nil, err
	}

	ctx := &InitialTeamContext{
		TeamName:      opts.TeamName,
		TeamFilePath:  GetTeamFilePath(opts.TeamName),
		LeadAgentID:   tf.LeadAgentID,
		SelfAgentName: opts.AgentName,
		IsLeader:      opts.AgentID == TeamLeadName || opts.AgentName == TeamLeadName,
		Teammates:     make(map[string]TeamMember, len(tf.Members)),
	}
	for _, member := range tf.Members {
		ctx.Teammates[member.AgentID] = member
	}
	if ctx.IsLeader {
		return ctx, nil
	}

	var member *TeamMember
	if opts.AgentID != "" {
		member, err = FindMemberByAgentID(opts.TeamName, opts.AgentID)
		if err != nil {
			return nil, err
		}
	}
	if member == nil {
		member, err = FindMemberByName(opts.TeamName, opts.AgentName)
		if err != nil {
			return nil, err
		}
	}
	if member != nil {
		ctx.SelfAgentID = member.AgentID
		ctx.SelfAgentName = member.Name
		ctx.BackendType = BackendType(member.BackendType)
		ctx.SessionID = member.SessionID
		ctx.Mode = member.Mode
		ctx.Color = member.Color
		ctx.PlanModeRequired = member.PlanModeRequired
		return ctx, nil
	}

	ctx.SelfAgentID = opts.AgentID
	return ctx, nil
}

package swarm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GetTeamsDir returns ~/.claude/teams/
func GetTeamsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "teams")
}

// sanitizeName replaces non-alphanumeric chars with '-' and lowercases.
func sanitizeName(name string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	return re.ReplaceAllString(strings.ToLower(name), "-")
}

// GetTeamDir returns ~/.claude/teams/{sanitized-name}/
func GetTeamDir(teamName string) string {
	return filepath.Join(GetTeamsDir(), sanitizeName(teamName))
}

// GetTeamFilePath returns ~/.claude/teams/{sanitized-name}/config.json
func GetTeamFilePath(teamName string) string {
	return filepath.Join(GetTeamDir(teamName), "config.json")
}

// ReadTeamFile reads and parses the team config file. Returns nil on not-found.
func ReadTeamFile(teamName string) (*TeamFile, error) {
	path := GetTeamFilePath(teamName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ReadTeamFile: %w", err)
	}
	var tf TeamFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("ReadTeamFile: parse %s: %w", path, err)
	}
	return &tf, nil
}

// WriteTeamFile writes the team config atomically.
func WriteTeamFile(teamName string, tf *TeamFile) error {
	dir := GetTeamDir(teamName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("WriteTeamFile: mkdir: %w", err)
	}
	path := GetTeamFilePath(teamName)
	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("WriteTeamFile: marshal: %w", err)
	}
	// Atomic write via temp file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("WriteTeamFile: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// CreateTeamFile creates a new team config file.
func CreateTeamFile(teamName, description, leadAgentID, leadSessionID string) (*TeamFile, error) {
	tf := &TeamFile{
		Name:          teamName,
		Description:   description,
		CreatedAt:     time.Now().UnixMilli(),
		LeadAgentID:   leadAgentID,
		LeadSessionID: leadSessionID,
		Members:       []TeamMember{},
	}
	if err := WriteTeamFile(teamName, tf); err != nil {
		return nil, err
	}
	return tf, nil
}

// AddMember appends a member to the team config and saves it.
func AddMember(teamName string, member TeamMember) error {
	tf, err := ReadTeamFile(teamName)
	if err != nil {
		return err
	}
	if tf == nil {
		return fmt.Errorf("AddMember: team %q not found", teamName)
	}
	tf.Members = append(tf.Members, member)
	return WriteTeamFile(teamName, tf)
}

// RemoveMemberByAgentID removes an in-process member by agentId.
func RemoveMemberByAgentID(teamName, agentID string) (bool, error) {
	tf, err := ReadTeamFile(teamName)
	if err != nil || tf == nil {
		return false, err
	}
	orig := len(tf.Members)
	filtered := tf.Members[:0]
	for _, m := range tf.Members {
		if m.AgentID != agentID {
			filtered = append(filtered, m)
		}
	}
	tf.Members = filtered
	if len(tf.Members) == orig {
		return false, nil
	}
	return true, WriteTeamFile(teamName, tf)
}

// SetMemberActive updates a member's isActive flag.
func SetMemberActive(teamName, memberName string, active bool) error {
	tf, err := ReadTeamFile(teamName)
	if err != nil || tf == nil {
		return err
	}
	for i := range tf.Members {
		if tf.Members[i].Name == memberName {
			tf.Members[i].IsActive = active
		}
	}
	return WriteTeamFile(teamName, tf)
}

// SetMemberMode updates a member's permission mode.
func SetMemberMode(teamName, memberName, mode string) error {
	tf, err := ReadTeamFile(teamName)
	if err != nil || tf == nil {
		return err
	}
	for i := range tf.Members {
		if tf.Members[i].Name == memberName {
			tf.Members[i].Mode = mode
		}
	}
	return WriteTeamFile(teamName, tf)
}

// ListTeams returns all team names that have a config.json.
func ListTeams() ([]string, error) {
	dir := GetTeamsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ListTeams: %w", err)
	}
	var teams []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		configPath := filepath.Join(dir, e.Name(), "config.json")
		if _, err := os.Stat(configPath); err == nil {
			teams = append(teams, e.Name())
		}
	}
	return teams, nil
}

// IsTeamLeader returns true if agentID is empty or equals "team-lead".
func IsTeamLeader(agentID string) bool {
	return agentID == "" || agentID == TeamLeadName
}

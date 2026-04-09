package coordinator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ding/claude-code/claude-go/internal/public/fsutil"
)

type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type TeamManager struct {
	path string
}

func NewTeamManager(root string) *TeamManager {
	return &TeamManager{path: filepath.Join(root, ".claude-go-teams.json")}
}

func (m *TeamManager) Create(name string) (Team, error) {
	teams, err := m.load()
	if err != nil {
		return Team{}, err
	}
	team := Team{
		ID:        time.Now().UTC().Format("20060102T150405.000000000Z"),
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	teams = append(teams, team)
	return team, m.save(teams)
}

func (m *TeamManager) Delete(name string) (bool, error) {
	teams, err := m.load()
	if err != nil {
		return false, err
	}
	filtered := teams[:0]
	removed := false
	for _, team := range teams {
		if team.Name == name {
			removed = true
			continue
		}
		filtered = append(filtered, team)
	}
	return removed, m.save(filtered)
}

func (m *TeamManager) List() ([]Team, error) { return m.load() }

func (m *TeamManager) load() ([]Team, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Team{}, nil
		}
		return nil, err
	}
	var teams []Team
	if err := json.Unmarshal(data, &teams); err != nil {
		return nil, err
	}
	return teams, nil
}

func (m *TeamManager) save(teams []Team) error {
	data, err := json.MarshalIndent(teams, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(m.path, data, 0o644)
}

package skills

import (
	"path/filepath"
	"testing"
)

func TestProjectOfficeSkillsLoad(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	loaded, err := NewSkillLoader().LoadSkillsFromDirectory(filepath.Join(repoRoot, ".claude", "skills"), SourceFile)
	if err != nil {
		t.Fatalf("load project skills: %v", err)
	}

	byName := make(map[string]*SkillDefinition, len(loaded))
	for i := range loaded {
		byName[loaded[i].Name] = loaded[i]
	}

	for _, name := range []string{"documents", "pdf", "presentations", "spreadsheets"} {
		skill := byName[name]
		if skill == nil {
			t.Fatalf("expected project office skill %q to load", name)
		}
		if !skill.RunAsJob {
			t.Fatalf("expected office skill %q to run as job", name)
		}
		agentapi, _ := skill.Metadata["agentapi"].(map[string]any)
		if produces, _ := agentapi["produces_artifacts"].(bool); !produces {
			t.Fatalf("expected office skill %q to produce artifacts", name)
		}
	}
}

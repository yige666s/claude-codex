package skills

import (
	"os"
	"path/filepath"
	"strings"
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

	documents := byName["documents"]
	if documents == nil {
		t.Fatal("expected documents skill to load")
	}
	for _, want := range []string{
		"Artifact` tool is only for final user-facing deliverables",
		"Do not use `Artifact` for intermediate Python scripts",
		"create it in a writable workspace or temp directory with the `Bash` tool",
		"call `Artifact` with `file_path` for the final `.docx` only",
		"Simple DOCX Creation Fast Path",
		"scripts/create_docx_artifact.py",
	} {
		if !strings.Contains(documents.Content, want) {
			t.Fatalf("documents skill content missing %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude", "skills", "documents", "scripts", "create_docx_artifact.py")); err != nil {
		t.Fatalf("expected documents create_docx_artifact.py helper: %v", err)
	}
}

package cli

import (
	"testing"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/skills"
)

func TestBuildCommandMatrixIncludesSlashAndSkillCommands(t *testing.T) {
	manager := skills.NewSkillManager()
	if err := manager.RegisterLoadedSkills([]*skills.SkillDefinition{
		{
			Name:                        "project-skill",
			Aliases:                     []string{"ps"},
			Description:                 "Project skill",
			ArgumentHint:                "<path>",
			UserInvocable:               true,
			HasUserSpecifiedDescription: true,
			Source:                      skills.SourceFile,
			LoadedFrom:                  string(skills.LoadedFromSkills),
			AllowedTools:                []string{"Read", "Grep"},
			Model:                       "inherit-test",
		},
		{
			Name:                        "bundled-skill",
			Description:                 "Bundled skill",
			UserInvocable:               true,
			HasUserSpecifiedDescription: true,
			Source:                      skills.SourceBundled,
			LoadedFrom:                  string(skills.LoadedFromBundled),
		},
	}); err != nil {
		t.Fatalf("register skills: %v", err)
	}

	matrix, err := BuildCommandMatrix(commandRegistry, manager, t.TempDir())
	if err != nil {
		t.Fatalf("BuildCommandMatrix() error = %v", err)
	}

	help := matrix.Find("/help")
	if help == nil {
		t.Fatal("expected /help in command matrix")
	}
	if help.Type != CommandTypeBuiltin || help.Source != CommandSourceBuiltin {
		t.Fatalf("unexpected help command metadata: %+v", help)
	}

	project := matrix.Find("/project-skill")
	if project == nil {
		t.Fatal("expected project skill in command matrix")
	}
	if project.Type != CommandTypePrompt || project.Kind != CommandKindSkill || project.Source != CommandSourceUser {
		t.Fatalf("unexpected project skill metadata: %+v", project)
	}
	if project.Usage != "<path>" || project.Model != "inherit-test" || len(project.AllowedTools) != 2 {
		t.Fatalf("expected skill execution metadata to be preserved, got %+v", project)
	}
	if matrix.Find("ps") != project {
		t.Fatal("expected skill alias lookup to resolve")
	}

	bundled := matrix.Find("bundled-skill")
	if bundled == nil || bundled.Source != CommandSourceBundled {
		t.Fatalf("expected bundled skill source metadata, got %+v", bundled)
	}
}

func TestCommandMatrixSkillToolFiltersMatchPromptCommandRules(t *testing.T) {
	manager := skills.NewSkillManager()
	if err := manager.RegisterLoadedSkills([]*skills.SkillDefinition{
		{
			Name:                        "model-facing",
			Description:                 "Model-facing skill",
			UserInvocable:               true,
			HasUserSpecifiedDescription: true,
			Source:                      skills.SourceFile,
			LoadedFrom:                  string(skills.LoadedFromSkills),
		},
		{
			Name:                        "slash-only",
			Description:                 "Slash-only skill",
			UserInvocable:               true,
			HasUserSpecifiedDescription: true,
			DisableModelInvocation:      true,
			Source:                      skills.SourceFile,
			LoadedFrom:                  string(skills.LoadedFromSkills),
		},
		{
			Name:          "plugin-without-description",
			Description:   "Derived plugin command",
			UserInvocable: true,
			Source:        skills.SourcePlugin,
			LoadedFrom:    string(skills.LoadedFromPlugin),
		},
	}); err != nil {
		t.Fatalf("register skills: %v", err)
	}

	matrix, err := BuildCommandMatrix(commandRegistry, manager, t.TempDir())
	if err != nil {
		t.Fatalf("BuildCommandMatrix() error = %v", err)
	}

	modelCommands := commandNames(GetSkillToolCommands(matrix))
	if !modelCommands["model-facing"] {
		t.Fatalf("expected model-facing skill in Skill tool commands, got %v", modelCommands)
	}
	if modelCommands["slash-only"] {
		t.Fatalf("did not expect disableModelInvocation skill in Skill tool commands, got %v", modelCommands)
	}
	if modelCommands["help"] {
		t.Fatalf("did not expect builtin help in Skill tool commands, got %v", modelCommands)
	}
	if modelCommands["plugin-without-description"] {
		t.Fatalf("did not expect plugin prompt without explicit description, got %v", modelCommands)
	}

	slashSkills := commandNames(GetSlashCommandToolSkills(matrix))
	if !slashSkills["model-facing"] || !slashSkills["slash-only"] {
		t.Fatalf("expected visible skills in slash skill filter, got %v", slashSkills)
	}
	if slashSkills["plugin-without-description"] {
		t.Fatalf("did not expect plugin prompt without explicit description in slash skill filter, got %v", slashSkills)
	}
}

func TestBuildRegistryIncludesTaskTodoSkillAndConfigTools(t *testing.T) {
	t.Setenv("CLAUDE_GO_ENABLE_TESTING_TOOLS", "true")
	cfg := config.Default()
	registry, err := buildRegistry(cfg, t.TempDir(), nil, skills.NewSkillManager())
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}

	names := map[string]bool{}
	for _, descriptor := range registry.Descriptors() {
		names[descriptor.Name] = true
	}

	for _, want := range []string{
		"Read",
		"Write",
		"Edit",
		"SendUserMessage",
		"SendUserFile",
		"Brief",
		"Config",
		"CronCreate",
		"CronDelete",
		"CronList",
		"PowerShell",
		"RemoteTrigger",
		"TestingPermission",
		"TaskCreate",
		"TaskGet",
		"TaskList",
		"TaskUpdate",
		"TaskStop",
		"TaskOutput",
		"TodoWrite",
		"Skill",
	} {
		if !names[want] {
			t.Fatalf("expected tool %s to be registered; got %v", want, names)
		}
	}
}

func TestBuildRegistryFiltersPrimitiveToolsWhenReplModeEnabled(t *testing.T) {
	t.Setenv("CLAUDE_CODE_REPL", "true")
	t.Setenv("CLAUDE_REPL_MODE", "true")
	cfg := config.Default()
	registry, err := buildRegistry(cfg, t.TempDir(), nil, skills.NewSkillManager())
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}

	names := map[string]bool{}
	for _, descriptor := range registry.Descriptors() {
		names[descriptor.Name] = true
	}

	for _, want := range []string{"REPL", "Config", "Skill"} {
		if !names[want] {
			t.Fatalf("expected tool %s to remain registered; got %v", want, names)
		}
	}
	for _, hidden := range []string{"Read", "Write", "Edit", "file_read", "file_write", "file_edit", "Glob", "Grep", "Bash", "NotebookEdit"} {
		if names[hidden] {
			t.Fatalf("expected primitive tool %s to be hidden in REPL mode; got %v", hidden, names)
		}
	}
}

func TestCombinedRegistryAdapterListUsesCommandMatrix(t *testing.T) {
	manager := skills.NewSkillManager()
	if err := manager.RegisterLoadedSkills([]*skills.SkillDefinition{
		{
			Name:                        "inspect",
			Aliases:                     []string{"i"},
			Description:                 "Inspect files",
			ArgumentHint:                "<target>",
			UserInvocable:               true,
			HasUserSpecifiedDescription: true,
			Source:                      skills.SourceFile,
			LoadedFrom:                  string(skills.LoadedFromSkills),
		},
	}); err != nil {
		t.Fatalf("register skills: %v", err)
	}

	adapter := NewCombinedRegistryAdapter(commandRegistry, manager, t.TempDir(), slashContext{})
	commands := adapter.List()
	byName := make(map[string]struct {
		description string
		usage       string
		aliases     []string
	})
	for _, cmd := range commands {
		byName[cmd.Name] = struct {
			description string
			usage       string
			aliases     []string
		}{description: cmd.Description, usage: cmd.Usage, aliases: cmd.Aliases}
	}

	help, ok := byName["/help"]
	if !ok {
		t.Fatal("expected /help in combined registry list")
	}
	if len(help.aliases) == 0 || help.aliases[0] != "/h" {
		t.Fatalf("expected slash-prefixed help aliases, got %v", help.aliases)
	}

	inspect, ok := byName["/inspect"]
	if !ok {
		t.Fatal("expected /inspect skill in combined registry list")
	}
	if inspect.usage != "<target>" {
		t.Fatalf("expected skill usage metadata, got %q", inspect.usage)
	}
	if inspect.description != "Inspect files (user)" {
		t.Fatalf("expected source-annotated skill description, got %q", inspect.description)
	}
}

func commandNames(commands []*CommandDef) map[string]bool {
	out := make(map[string]bool, len(commands))
	for _, cmd := range commands {
		out[cmd.Name] = true
	}
	return out
}

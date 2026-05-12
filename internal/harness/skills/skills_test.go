package skills

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSkillRegistry(t *testing.T) {
	registry := NewSkillRegistry()

	// Test registration
	skill := &SkillDefinition{
		Name:        "test-skill",
		Description: "Test skill",
		Aliases:     []string{"ts", "test"},
	}

	err := registry.Register(skill)
	if err != nil {
		t.Fatalf("failed to register skill: %v", err)
	}

	// Test retrieval by name
	retrieved, ok := registry.Get("test-skill")
	if !ok {
		t.Fatal("skill not found by name")
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", retrieved.Name)
	}

	// Test retrieval by alias
	retrieved, ok = registry.Get("ts")
	if !ok {
		t.Fatal("skill not found by alias")
	}
	if retrieved.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", retrieved.Name)
	}

	// Test duplicate registration
	err = registry.Register(skill)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}

	// Test count
	if registry.Count() != 1 {
		t.Errorf("expected count 1, got %d", registry.Count())
	}

	// Test removal
	removed := registry.Remove("test-skill")
	if !removed {
		t.Error("failed to remove skill")
	}

	if registry.Count() != 0 {
		t.Errorf("expected count 0 after removal, got %d", registry.Count())
	}
}

func TestSecurityValidation(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		shouldErr bool
	}{
		{"valid relative path", "file.txt", false},
		{"valid nested path", "dir/file.txt", false},
		{"absolute path", "/etc/passwd", true},
		{"parent traversal", "../file.txt", true},
		{"nested traversal", "dir/../../file.txt", true},
		{"double dot", "..", true},
	}

	baseDir := "/tmp/test"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateSkillPath(baseDir, tt.path)
			if tt.shouldErr && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestIsPathSafe(t *testing.T) {
	tests := []struct {
		path string
		safe bool
	}{
		{"file.txt", true},
		{"dir/file.txt", true},
		{"/absolute/path", false},
		{"../parent", false},
		{"dir/../file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsPathSafe(tt.path)
			if result != tt.safe {
				t.Errorf("expected %v, got %v", tt.safe, result)
			}
		})
	}
}

func TestSanitizeSkillName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"valid-name", "valid-name"},
		{"name/with/slashes", "name_with_slashes"},
		{"name:with:colons", "name_with_colons"},
		{"name*with*stars", "name_with_stars"},
		{"", "unnamed"},
		{"  spaces  ", "spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeSkillName(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := `---
name: Test Skill
description: A test skill
when_to_use: When testing
allowed-tools: ["Read", "Write"]
user-invocable: true
---

This is the skill content.
`

	parsed, err := ParseSkillFile(content)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if parsed.Frontmatter.Name != "Test Skill" {
		t.Errorf("expected name 'Test Skill', got '%s'", parsed.Frontmatter.Name)
	}

	if parsed.Content != "This is the skill content." {
		t.Errorf("unexpected content: %s", parsed.Content)
	}
}

func TestParseStringArray(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{"string with commas", "a, b, c", []string{"a", "b", "c"}},
		{"array", []interface{}{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"nil", nil, nil},
		{"empty string", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseStringArray(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("at index %d: expected '%s', got '%s'", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestParseSkillFileQuotesProblematicFrontmatterValues(t *testing.T) {
	content := `---
name: Glob Skill
paths: src/*.{ts,tsx}, packages/{api,web}/**
---

Body
`

	parsed, err := ParseSkillFile(content)
	if err != nil {
		t.Fatalf("failed to parse frontmatter with unquoted brace globs: %v", err)
	}
	paths := ParsePaths(parsed.Frontmatter.Paths)
	want := []string{"src/*.ts", "src/*.tsx", "packages/api", "packages/web"}
	if len(paths) != len(want) {
		t.Fatalf("expected paths %#v, got %#v", want, paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("path %d: expected %q, got %q", i, want[i], paths[i])
		}
	}
}

func TestParseArgumentNamesSplitsWhitespaceAndFiltersNumericNames(t *testing.T) {
	got := ParseArgumentNames("topic audience 2")
	want := []string{"topic", "audience"}
	if len(got) != len(want) {
		t.Fatalf("expected arguments %#v, got %#v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argument %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestParseEffortAcceptsMaxAlias(t *testing.T) {
	got := ParseEffort("max")
	if got == nil || *got != 5 {
		t.Fatalf("expected max effort to parse as 5, got %#v", got)
	}
}

func TestParseSkillMetadataEnv(t *testing.T) {
	allowed, primary := ParseSkillMetadataEnv(map[string]interface{}{
		"openclaw": map[string]interface{}{
			"primaryEnv": "SHORTART_API_KEY",
			"requires": map[string]interface{}{
				"env": []interface{}{"SHORTART_API_KEY", "OTHER_KEY"},
			},
		},
	})
	if primary != "SHORTART_API_KEY" {
		t.Fatalf("expected primary env, got %q", primary)
	}
	if len(allowed) != 2 || allowed[0] != "SHORTART_API_KEY" || allowed[1] != "OTHER_KEY" {
		t.Fatalf("unexpected allowed env %#v", allowed)
	}
}

func TestParseSkillMetadataRunAsJob(t *testing.T) {
	cases := []map[string]interface{}{
		{"job": true},
		{"run_as_job": "true"},
		{"long_running": "yes"},
		{"produces_artifacts": true},
		{"agentapi": map[string]interface{}{"execution": "job"}},
		{"runtime": map[string]interface{}{"run_as_job": true}},
		{"openclaw": map[string]interface{}{"long_running": true}},
	}
	for _, tc := range cases {
		if !ParseSkillMetadataRunAsJob(tc) {
			t.Fatalf("expected run-as-job metadata to parse true: %#v", tc)
		}
	}
	if ParseSkillMetadataRunAsJob(map[string]interface{}{"job": false}) {
		t.Fatal("expected false job metadata to stay false")
	}
}

func TestSkillLoader(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create a test skill file
	skillContent := `---
name: Test Skill
description: A test skill
metadata:
  product:
    category: Developer Tools
    icon: CODE
---

Test content
`

	skillPath := filepath.Join(tmpDir, "test-skill.md")
	err := os.WriteFile(skillPath, []byte(skillContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load skill
	loader := NewSkillLoader()
	skill, err := loader.LoadSkillFromFile(skillPath, SourceFile)
	if err != nil {
		t.Fatalf("failed to load skill: %v", err)
	}

	if skill.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", skill.Name)
	}

	if skill.DisplayName != "Test Skill" {
		t.Errorf("expected display name 'Test Skill', got '%s'", skill.DisplayName)
	}
	product, ok := skill.Metadata["product"].(map[string]any)
	if !ok || product["category"] != "Developer Tools" || product["icon"] != "CODE" {
		t.Fatalf("expected product metadata to be preserved, got %#v", skill.Metadata)
	}

	// Test cache
	cached := loader.cache.Get(skill.FileIdentity)
	if cached == nil {
		t.Error("skill not cached")
	}
}

func TestSkillPromptSubstitutesTypeScriptArgumentPlaceholders(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	content := `---
name: Demo
arguments: topic audience
---

Topic=$topic
First=$0
Second=$ARGUMENTS[1]
All=$ARGUMENTS
`
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	loader := NewSkillLoader()
	skill, err := loader.LoadSkillFromFile(skillPath, SourceFile)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	blocks, err := skill.GetPrompt(`hello "wide world"`, nil)
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	got := blocks[0].Text
	for _, want := range []string{
		"Topic=hello",
		"First=hello",
		"Second=wide world",
		`All=hello "wide world"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, got)
		}
	}
}

func TestSkillPromptAppendsArgumentsWhenNoPlaceholderExists(t *testing.T) {
	tmpDir := t.TempDir()
	skillPath := filepath.Join(tmpDir, "plain.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: Plain\n---\n\nDo the thing.\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	loader := NewSkillLoader()
	skill, err := loader.LoadSkillFromFile(skillPath, SourceFile)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	blocks, err := skill.GetPrompt("extra context", nil)
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if !strings.Contains(blocks[0].Text, "ARGUMENTS: extra context") {
		t.Fatalf("expected prompt to append raw arguments, got %q", blocks[0].Text)
	}
}

func TestSkillModelInheritMapsToDefaultModel(t *testing.T) {
	tmpDir := t.TempDir()
	skillPath := filepath.Join(tmpDir, "inherit.md")
	content := "---\nname: Inherit\nmodel: inherit\n---\n\nBody\n"
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	loader := NewSkillLoader()
	skill, err := loader.LoadSkillFromFile(skillPath, SourceFile)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	if skill.Model != "" {
		t.Fatalf("expected inherited model to be empty, got %q", skill.Model)
	}
}

func TestLoadSkillsFromDirectoryFollowsSymlinkedSkillDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "skills")
	target := filepath.Join(tmpDir, "actual-skill")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: Actual\n---\n\nBody\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked-skill")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	loader := NewSkillLoader()
	loaded, err := loader.LoadSkillsFromDirectory(root, SourceFile)
	if err != nil {
		t.Fatalf("LoadSkillsFromDirectory() error = %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "linked-skill" {
		t.Fatalf("expected linked skill to load as directory name, got %#v", loaded)
	}
}

func TestLoadCommandsFromDirectorySupportsLegacyMarkdownAndSkillDirs(t *testing.T) {
	tmpDir := t.TempDir()
	commandsDir := filepath.Join(tmpDir, "commands")
	if err := os.MkdirAll(filepath.Join(commandsDir, "git"), 0o755); err != nil {
		t.Fatalf("mkdir git commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(commandsDir, "workflow"), 0o755); err != nil {
		t.Fatalf("mkdir workflow command: %v", err)
	}
	files := map[string]string{
		filepath.Join(commandsDir, "review.md"):              "---\nname: Review\n---\n\nReview prompt\n",
		filepath.Join(commandsDir, "git", "commit.md"):       "---\nname: Commit\n---\n\nCommit prompt\n",
		filepath.Join(commandsDir, "workflow", "SKILL.md"):   "---\nname: Workflow\n---\n\nWorkflow prompt\n",
		filepath.Join(commandsDir, "workflow", "ignored.md"): "---\nname: Ignored\n---\n\nIgnored prompt\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	loader := NewSkillLoader()
	loaded, err := loader.LoadCommandsFromDirectory(commandsDir, SourceFile)
	if err != nil {
		t.Fatalf("LoadCommandsFromDirectory() error = %v", err)
	}
	byName := make(map[string]*SkillDefinition, len(loaded))
	for _, skill := range loaded {
		byName[skill.Name] = skill
	}
	for _, name := range []string{"review", "git:commit", "workflow"} {
		skill := byName[name]
		if skill == nil {
			t.Fatalf("expected command %q in %#v", name, byName)
		}
		if skill.LoadedFrom != string(LoadedFromCommands) {
			t.Fatalf("expected command %q loadedFrom=%q, got %q", name, LoadedFromCommands, skill.LoadedFrom)
		}
	}
	if byName["workflow:ignored"] != nil {
		t.Fatal("expected SKILL.md directory command to suppress sibling markdown files")
	}
}

func TestSkillManagerUnsubscribeRemovesListener(t *testing.T) {
	manager := NewSkillManager()
	unsubscribe := manager.OnSkillsChanged(func() {})
	unsubscribe()

	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if len(manager.listeners) != 0 {
		t.Fatalf("expected listener to be removed, got %d listeners", len(manager.listeners))
	}
}

func TestWrapGeneratedSkillPromptIncludesMetadata(t *testing.T) {
	prompt := WrapGeneratedSkillPrompt("demo-skill", "hello world", "body")
	for _, want := range []string{
		"<command-message>demo-skill</command-message>",
		"<command-name>/demo-skill</command-name>",
		"<command-args>hello world</command-args>",
		"body",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected wrapped prompt to contain %q, got %q", want, prompt)
		}
	}
}

func TestSkillLoaderExecutesShellCommandsInPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print('hello-from-shell')\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	content := "---\n" +
		"name: Demo\n" +
		"shell: bash\n" +
		"allowed-tools: Bash(python3 *)\n" +
		"---\n\n" +
		"Result:\n" +
		"```!\n" +
		"python3 scripts/run.py\n" +
		"```\n"
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	loader := NewSkillLoader()
	skill, err := loader.LoadSkillFromFile(skillPath, SourceFile)
	if err != nil {
		t.Fatalf("load skill: %v", err)
	}
	blocks, err := skill.GetPrompt("", &SkillContext{WorkingDir: skillDir})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if len(blocks) == 0 || !strings.Contains(blocks[0].Text, "hello-from-shell") {
		t.Fatalf("expected executed shell output in prompt, got %#v", blocks)
	}
}

func TestExecuteShellCommandsInPromptFallsBackToPython3(t *testing.T) {
	originalLookPath := lookPath
	defer func() { lookPath = originalLookPath }()
	lookPath = func(file string) (string, error) {
		switch file {
		case "python":
			return "", exec.ErrNotFound
		case "python3":
			return "/usr/bin/python3", nil
		default:
			return originalLookPath(file)
		}
	}

	workingDir := t.TempDir()
	scriptsDir := filepath.Join(workingDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print(42)\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := ExecuteShellCommandsInPrompt("```!\npython scripts/run.py\n```", ShellBash, workingDir, nil, []string{"Bash(python3 *)"}, nil)
	if err != nil {
		t.Fatalf("ExecuteShellCommandsInPrompt() error = %v", err)
	}
	if !strings.Contains(result, "42") {
		t.Fatalf("expected python3 fallback output, got %q", result)
	}

	if err := os.WriteFile(filepath.Join(scriptsDir, "other.py"), []byte("print(7)\n"), 0o644); err != nil {
		t.Fatalf("write second script: %v", err)
	}
	result, err = ExecuteShellCommandsInPrompt("```!\npython scripts/other.py\n```", ShellBash, workingDir, nil, []string{"Bash(python3 *)"}, nil)
	if err != nil {
		t.Fatalf("ExecuteShellCommandsInPrompt() second script error = %v", err)
	}
	if !strings.Contains(result, "7") {
		t.Fatalf("expected compound python3 fallback output, got %q", result)
	}
}

func TestExecuteShellCommandsInPromptRejectsCommandOutsideAllowedTools(t *testing.T) {
	workingDir := t.TempDir()
	_, err := ExecuteShellCommandsInPrompt("```!\necho blocked\n```", ShellBash, workingDir, nil, []string{"Bash(printf:*)"}, rejectingRuntime{})
	if err == nil {
		t.Fatal("expected command outside allowed-tools to be rejected")
	}
	if !strings.Contains(err.Error(), "web sandbox denied command") {
		t.Fatalf("expected sandbox error, got %v", err)
	}
}

func TestExecuteShellCommandsInPromptRejectsLocalCommandOutsideAllowedTools(t *testing.T) {
	workingDir := t.TempDir()
	_, err := ExecuteShellCommandsInPrompt("```!\necho blocked\n```", ShellBash, workingDir, nil, []string{"Bash(printf *)"}, nil)
	if err == nil {
		t.Fatal("expected local command outside allowed-tools to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected allowed-tools error, got %v", err)
	}
}

func TestExecuteShellCommandsInPromptTimesOut(t *testing.T) {
	workingDir := t.TempDir()
	_, err := ExecuteShellCommandsInPromptWithTimeout("```!\nsleep 1\n```", ShellBash, workingDir, nil, nil, nil, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected shell timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestExecuteShellCommandsInPromptAllowsLocalShellWithoutWebSandbox(t *testing.T) {
	workingDir := t.TempDir()
	result, err := ExecuteShellCommandsInPrompt("```!\necho local-ok\n```", ShellBash, workingDir, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected local shell execution to succeed, got %v", err)
	}
	if !strings.Contains(result, "local-ok") {
		t.Fatalf("expected local shell output, got %q", result)
	}
}

func TestExecuteShellCommandsInPromptAllowsSkillScopedScriptsDiscovery(t *testing.T) {
	workingDir := t.TempDir()
	scriptsDir := filepath.Join(workingDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "run.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	result, err := ExecuteShellCommandsInPrompt("```!\nfind scripts -name \"*.py\"\n```", ShellBash, workingDir, nil, nil, nil)
	if err != nil {
		t.Fatalf("expected scripts discovery to pass, got %v", err)
	}
	if !strings.Contains(result, "scripts/run.py") {
		t.Fatalf("expected scripts discovery output, got %q", result)
	}
}

type rejectingRuntime struct{}

func (rejectingRuntime) ExecuteCommand(ctx context.Context, command string) (string, error) {
	return "", skillsRejected(command)
}

func (rejectingRuntime) ValidateCommand(command string) error {
	return skillsRejected(command)
}

type skillsRejected string

func (e skillsRejected) Error() string {
	return "web sandbox denied command " + string(e)
}

func TestSkillManager(t *testing.T) {
	manager := NewSkillManager()

	// Register a bundled skill
	skill := NewSimpleSkill("test", "Test skill", "Test content")
	err := RegisterBundledSkill(skill)
	if err != nil {
		t.Fatalf("failed to register bundled skill: %v", err)
	}

	// Load bundled skills
	err = manager.LoadBundledSkills()
	if err != nil {
		t.Fatalf("failed to load bundled skills: %v", err)
	}

	// Retrieve skill
	retrieved, ok := manager.GetSkill("test")
	if !ok {
		t.Fatal("skill not found")
	}

	if retrieved.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", retrieved.Name)
	}

	// Test stats
	stats := manager.GetStats()
	if stats.TotalSkills == 0 {
		t.Error("expected at least one skill")
	}

	// Cleanup
	ClearBundledSkills()
}

func TestMatchUserInvocableSkillByTriggerPhrase(t *testing.T) {
	manager := NewSkillManager()
	skill := NewSimpleSkill("shortart-image-generator-openclaw", "Generate images. Triggers on: 生成图片, create image, draw", "body")
	if err := manager.registry.Register(skill); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	matched, ok := manager.MatchUserInvocableSkill("帮我生成图片")
	if !ok || matched == nil || matched.Name != "shortart-image-generator-openclaw" {
		t.Fatalf("expected trigger phrase to match image skill, got %#v ok=%v", matched, ok)
	}
}

func TestMatchUserInvocableSkillByTriggersInclude(t *testing.T) {
	manager := NewSkillManager()
	skill := NewSimpleSkill("docx", "Create documents. Triggers include: 'Word doc', '生成文档', '整理为一个文档', '调研文档'", "body")
	if err := manager.registry.Register(skill); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	matched, ok := manager.MatchUserInvocableSkill("搜索资料并整理为一个文档")
	if !ok || matched == nil || matched.Name != "docx" {
		t.Fatalf("expected triggers include phrase to match docx skill, got %#v ok=%v", matched, ok)
	}

	matched, ok = manager.MatchUserInvocableSkill("搜索一下这个网站，把活动资讯整理一下，生成一个文档")
	if !ok || matched == nil || matched.Name != "docx" {
		t.Fatalf("expected normalized document phrase to match docx skill, got %#v ok=%v", matched, ok)
	}
}

func TestConditionalActivation(t *testing.T) {
	manager := NewSkillManager()

	// Create a conditional skill
	skill := &SkillDefinition{
		Name:        "conditional-skill",
		Description: "Conditional skill",
		Paths:       []string{"src/**"},
		Source:      SourceFile,
	}

	manager.mu.Lock()
	manager.conditionalSkills[skill.Name] = skill
	manager.mu.Unlock()

	// Activate for matching path
	activated := manager.ActivateConditionalSkillsForPaths([]string{"src/main.go"})
	if activated != 1 {
		t.Errorf("expected 1 activation, got %d", activated)
	}

	// Check if skill is now active
	_, ok := manager.GetSkill("conditional-skill")
	if !ok {
		t.Error("skill should be active")
	}

	// Check conditional count
	count := manager.GetConditionalSkillCount()
	if count != 0 {
		t.Errorf("expected 0 conditional skills, got %d", count)
	}
}

func TestMCPSkills(t *testing.T) {
	// Register default builder
	RegisterMCPSkillBuilder(DefaultMCPSkillBuilder)

	// Build MCP skill
	metadata := map[string]interface{}{
		"name":        "test-tool",
		"description": "Test MCP tool",
	}

	skill, err := BuildMCPSkill("test-tool", metadata)
	if err != nil {
		t.Fatalf("failed to build MCP skill: %v", err)
	}

	if skill.Name != "test-tool" {
		t.Errorf("expected name 'test-tool', got '%s'", skill.Name)
	}

	if skill.Source != SourceMCP {
		t.Errorf("expected source MCP, got %s", skill.Source)
	}
}

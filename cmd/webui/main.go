package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/tools"
	filetool "claude-codex/internal/harness/tools/file"
	searchtool "claude-codex/internal/harness/tools/search"
	skilltool "claude-codex/internal/harness/tools/skill"
	webtool "claude-codex/internal/harness/tools/web"
	websandbox "claude-codex/internal/harness/websandbox"
	"claude-codex/internal/ui/web/server"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP server address")
	apiKey := flag.String("api-key", "sk-Y5wpnBQUIMmFtSTqN9tXfSxyi3Rb8ebMShfBNynRNjoD3zkv", "API key (or set ANTHROPIC_API_KEY)")
	apiBaseURL := flag.String("api-base-url", "https://hk.linkapi.ai", "API base URL (or set ANTHROPIC_BASE_URL, default: https://api.anthropic.com)")
	model := flag.String("model", "claude-sonnet-4-6", "Model to use")
	flag.Parse()

	// Get API key/token from flags or environment
	key := *apiKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}

	// Prefer token over key if both provided
	credential := key
	if credential == "" {
		log.Fatal("API key or token required: use -api-key/-api-token flag or ANTHROPIC_API_KEY/ANTHROPIC_API_TOKEN environment variable")
	}

	// Get base URL from flag or environment
	baseURL := *apiBaseURL

	// Get working directory
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}

	// Initialize skill manager and load skills
	skillManager := skills.NewSkillManager()

	// Load bundled skills
	if err := skillManager.LoadBundledSkills(); err != nil {
		log.Printf("Warning: failed to load bundled skills: %v", err)
	}

	// Load user skills from ~/.claude/skills
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userSkillsDir := filepath.Join(homeDir, ".claude", "skills")
		if err := skillManager.LoadSkillsFromDirectory(userSkillsDir, skills.SourceFile); err != nil {
			log.Printf("Info: no user skills loaded from %s", userSkillsDir)
		} else {
			log.Printf("Loaded user skills from %s", userSkillsDir)
		}
	}

	// Load project skills from ./.claude/skills
	projectSkillsDir := filepath.Join(workingDir, ".claude", "skills")
	if err := skillManager.LoadSkillsFromDirectory(projectSkillsDir, skills.SourceFile); err != nil {
		log.Printf("Info: no project skills loaded from %s", projectSkillsDir)
	} else {
		log.Printf("Loaded project skills from %s", projectSkillsDir)
	}

	// Log skill stats
	stats := skillManager.GetStats()
	log.Printf("Skills loaded: %d total (%d bundled, %d dynamic, %d user-invocable)",
		stats.TotalSkills, stats.BundledSkills, stats.DynamicSkills, stats.UserInvocable)

	registryBuilder := newRegistryBuilder(skillManager)
	sandboxFactory := newSandboxFactory()

	// Create and start server
	srv := server.New(credential, baseURL, *model, registryBuilder, sandboxFactory, skillManager)
	log.Printf("Starting Web UI server on %s", *addr)
	log.Printf("Using API base URL: %s", baseURL)
	log.Printf("Using model: %s", *model)
	log.Printf("Open http://localhost%s in your browser", *addr)

	if err := srv.Start(*addr); err != nil {
		log.Fatal(err)
	}
}

func newRegistryBuilder(skillManager *skills.SkillManager) func(websandbox.Scope) *tools.Registry {
	sandboxFactory := newSandboxFactory()
	return func(scope websandbox.Scope) *tools.Registry {
		toolsList := []tools.Tool{
			websandbox.NewBashTool(scope, sandboxFactory(scope)),
			filetool.NewReadTool(scope.RootDir),
			filetool.NewWriteTool(scope.RootDir),
			filetool.NewEditTool(scope.RootDir),
			searchtool.NewGlobTool(scope.RootDir),
			searchtool.NewGrepTool(scope.RootDir),
			webtool.NewSearchTool(nil),
			webtool.NewFetchTool(nil),
			skilltool.NewTool(skillManager),
		}
		return tools.NewRegistry(toolsList...)
	}
}

func newSandboxFactory() func(websandbox.Scope) websandbox.RuntimeOptions {
	image := strings.TrimSpace(os.Getenv("CLAUDE_GO_WEB_SANDBOX_IMAGE"))
	return func(scope websandbox.Scope) websandbox.RuntimeOptions {
		return websandbox.RuntimeOptions{
			Image:          image,
			Timeout:        2 * time.Minute,
			NetworkEnabled: scope.SkillScoped,
			AutoBuildImage: true,
		}
	}
}

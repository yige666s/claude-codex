package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/ding/claude-code/claude-go/internal/harness/skills"
	"github.com/ding/claude-code/claude-go/internal/harness/tools"
	bashtool "github.com/ding/claude-code/claude-go/internal/harness/tools/bash"
	filetool "github.com/ding/claude-code/claude-go/internal/harness/tools/file"
	searchtool "github.com/ding/claude-code/claude-go/internal/harness/tools/search"
	skilltool "github.com/ding/claude-code/claude-go/internal/harness/tools/skill"
	webtool "github.com/ding/claude-code/claude-go/internal/harness/tools/web"
	"github.com/ding/claude-code/claude-go/internal/ui/web/server"
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

	// Create tool registry with basic tools
	toolsList := []tools.Tool{
		bashtool.NewTool(workingDir),
		filetool.NewReadTool(workingDir),
		filetool.NewWriteTool(workingDir),
		filetool.NewEditTool(workingDir),
		searchtool.NewGlobTool(workingDir),
		searchtool.NewGrepTool(workingDir),
		webtool.NewSearchTool(nil),
		webtool.NewFetchTool(nil),
		skilltool.NewTool(skillManager),
	}

	registry := tools.NewRegistry(toolsList...)

	// Create and start server
	srv := server.New(credential, baseURL, *model, registry, skillManager)
	log.Printf("Starting Web UI server on %s", *addr)
	log.Printf("Using API base URL: %s", baseURL)
	log.Printf("Using model: %s", *model)
	log.Printf("Open http://localhost%s in your browser", *addr)

	if err := srv.Start(*addr); err != nil {
		log.Fatal(err)
	}
}

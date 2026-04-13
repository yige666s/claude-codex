package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"claude-codex/internal/harness/anthropic"
	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/harness/websandbox"
)

//go:embed static/index.html
var staticFiles embed.FS

type Server struct {
	engine          *engine.Engine
	registryBuilder func(websandbox.Scope) *toolkit.Registry
	sandboxFactory  func(websandbox.Scope) websandbox.RuntimeOptions
	apiKey          string
	baseURL         string
	model           string
	debug           bool
	skillManager    *skills.SkillManager
	sessions        map[string]*state.Session
	mu              sync.RWMutex
	upgrader        websocket.Upgrader
	workingDir      string
}

type Message struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Role      string          `json:"role,omitempty"`
	Error     string          `json:"error,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func New(apiKey, baseURL, model string, registryBuilder func(websandbox.Scope) *toolkit.Registry, sandboxFactory func(websandbox.Scope) websandbox.RuntimeOptions, skillManager *skills.SkillManager) *Server {
	client := anthropic.NewClient(apiKey, baseURL, 30*time.Second)
	planner := anthropic.NewPlanner(client, model)
	checker := permissions.NewChecker(permissions.ModeBypass, nil, nil)

	// Get working directory
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}

	defaultScope := websandbox.Scope{RootDir: workingDir}
	registry := registryBuilder(defaultScope)
	eng := engine.NewWithDir(planner, registry, checker, 0, workingDir)
	eng.SetSkillManager(skillManager)

	return &Server{
		engine:          eng,
		registryBuilder: registryBuilder,
		sandboxFactory:  sandboxFactory,
		apiKey:          apiKey,
		baseURL:         baseURL,
		model:           model,
		debug:           isDebugEnabled(),
		skillManager:    skillManager,
		sessions:        make(map[string]*state.Session),
		workingDir:      workingDir,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for development
			},
		},
	}
}

func (s *Server) getOrCreateSession(sessionID string) *state.Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, ok := s.sessions[sessionID]; ok {
		return session
	}

	session := state.NewSession(s.workingDir)
	s.sessions[sessionID] = session
	return session
}

func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	s.debugf("webui websocket connected session_id=%s remote=%s", sessionID, r.RemoteAddr)
	defer s.debugf("webui websocket disconnected session_id=%s", sessionID)

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			s.debugf("webui read error session_id=%s error=%v", sessionID, err)
			break
		}

		if msg.Type == "chat" {
			s.debugf("webui received chat session_id=%s chars=%d slash=%t", sessionID, len(msg.Content), strings.HasPrefix(strings.TrimSpace(msg.Content), "/"))
			s.handleChat(conn, sessionID, msg.Content)
		} else {
			s.debugf("webui ignored message session_id=%s type=%s", sessionID, msg.Type)
		}
	}
}

func (s *Server) handleChat(conn *websocket.Conn, sessionID, prompt string) {
	start := time.Now()
	session := s.getOrCreateSession(sessionID)

	// Send user message
	conn.WriteJSON(Message{
		Type:    "message",
		Role:    "user",
		Content: prompt,
	})

	// Check if it's a skill invocation (starts with /)
	if strings.HasPrefix(strings.TrimSpace(prompt), "/") {
		s.handleSkillCommand(conn, session, prompt)
		return
	}
	if skill, ok := s.skillManager.MatchUserInvocableSkill(prompt); ok {
		s.debugf("webui auto-matched skill session_id=%s skill=%s", sessionID, skill.Name)
		s.executeSkill(conn, session, skill, prompt)
		return
	}

	// Run engine and stream response
	ctx := context.Background()
	result, err := s.engine.Run(ctx, session, prompt)
	if err != nil {
		log.Printf("webui engine error session_id=%s duration=%s error=%v", sessionID, time.Since(start), err)
		conn.WriteJSON(Message{
			Type:  "error",
			Error: err.Error(),
		})
		return
	}

	// Send assistant response
	conn.WriteJSON(Message{
		Type:    "message",
		Role:    "assistant",
		Content: result.Output,
	})

	conn.WriteJSON(Message{
		Type: "done",
	})
	s.debugf("webui chat completed session_id=%s duration=%s output_chars=%d", sessionID, time.Since(start), len(result.Output))
}

// handleSkillCommand handles skill invocations like /commit, /review, etc.
func (s *Server) handleSkillCommand(conn *websocket.Conn, session *state.Session, prompt string) {
	start := time.Now()
	// Parse command: "/skillname args..."
	parts := strings.SplitN(strings.TrimSpace(prompt), " ", 2)
	skillName := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	s.debugf("webui skill requested session_id=%s skill=%s args_chars=%d", session.ID, skillName, len(args))

	// Special command: /skills - list all available skills
	if skillName == "skills" {
		s.debugf("webui list skills session_id=%s", session.ID)
		s.handleListSkills(conn)
		return
	}

	// Get skill
	skill, ok := s.skillManager.GetSkill(skillName)
	if !ok {
		log.Printf("webui skill not found session_id=%s skill=%s", session.ID, skillName)
		conn.WriteJSON(Message{
			Type:  "error",
			Error: fmt.Sprintf("Unknown skill: /%s. Use /skills to list available skills.", skillName),
		})
		conn.WriteJSON(Message{Type: "done"})
		return
	}

	// Check if user can invoke this skill
	if !skill.UserInvocable {
		log.Printf("webui skill not invocable session_id=%s skill=%s", session.ID, skillName)
		conn.WriteJSON(Message{
			Type:  "error",
			Error: fmt.Sprintf("Skill /%s is not user-invocable.", skillName),
		})
		conn.WriteJSON(Message{Type: "done"})
		return
	}

	s.executeSkill(conn, session, skill, args)
	s.debugf("webui skill dispatch session_id=%s skill=%s duration=%s", session.ID, skillName, time.Since(start))
}

func (s *Server) executeSkill(conn *websocket.Conn, session *state.Session, skill *skills.SkillDefinition, args string) {
	start := time.Now()
	skillDir := effectiveSkillWorkingDir(skill, s.workingDir)
	scope := s.scopeForSkill(session, skill, skillDir)
	sandboxRuntime := websandbox.NewRuntime(scope, s.sandboxOptions(scope))

	// Generate prompt from skill
	blocks, err := skill.GetPrompt(args, &skills.SkillContext{
		SessionID:  session.ID,
		WorkingDir: skillDir,
		WebSandbox: sandboxRuntime,
	})
	if err != nil {
		log.Printf("webui skill prompt error session_id=%s skill=%s duration=%s error=%v", session.ID, skill.Name, time.Since(start), err)
		conn.WriteJSON(Message{
			Type:  "error",
			Error: fmt.Sprintf("Failed to generate skill prompt: %v", err),
		})
		conn.WriteJSON(Message{Type: "done"})
		return
	}

	// Convert blocks to text
	var promptText string
	for _, block := range blocks {
		if block.Type == "text" {
			promptText += block.Text
		}
	}
	promptText = websandbox.BuildTrustedPrompt(scope, promptText, args, collectSkillScripts(skillDir))
	promptText = skills.WrapGeneratedSkillPrompt(skill.Name, args, promptText)
	s.debugf("webui skill prompt ready session_id=%s skill=%s skill_root=%s prompt_chars=%d", session.ID, skill.Name, skill.SkillRoot, len(promptText))

	// Run engine with skill prompt. File-based skills may need access to their
	// own directory for references/scripts, so use the skill root when present.
	engineForSkill := s.engineForScope(scope)
	ctx := context.Background()
	result, err := engineForSkill.RunGeneratedPrompt(ctx, session, promptText)
	if err != nil {
		log.Printf("webui skill engine error session_id=%s skill=%s duration=%s error=%v", session.ID, skill.Name, time.Since(start), err)
		conn.WriteJSON(Message{
			Type:  "error",
			Error: err.Error(),
		})
		return
	}

	// Send assistant response
	conn.WriteJSON(Message{
		Type:    "message",
		Role:    "assistant",
		Content: result.Output,
	})

	conn.WriteJSON(Message{
		Type: "done",
	})
	s.debugf("webui skill completed session_id=%s skill=%s duration=%s output_chars=%d", session.ID, skill.Name, time.Since(start), len(result.Output))
}

// handleListSkills lists all available skills
func (s *Server) handleListSkills(conn *websocket.Conn) {
	userSkills := s.skillManager.ListUserInvocableSkills()

	if len(userSkills) == 0 {
		conn.WriteJSON(Message{
			Type:    "message",
			Role:    "assistant",
			Content: "No skills available.",
		})
		conn.WriteJSON(Message{Type: "done"})
		return
	}

	// Build skills list with better formatting
	var output strings.Builder
	output.WriteString("# Available Skills\n\n")

	// Group by source
	bundledSkills := make([]*skills.SkillDefinition, 0)
	fileSkills := make([]*skills.SkillDefinition, 0)
	otherSkills := make([]*skills.SkillDefinition, 0)

	for _, skill := range userSkills {
		switch skill.Source {
		case skills.SourceBundled:
			bundledSkills = append(bundledSkills, skill)
		case skills.SourceFile:
			fileSkills = append(fileSkills, skill)
		default:
			otherSkills = append(otherSkills, skill)
		}
	}

	// Display bundled skills
	if len(bundledSkills) > 0 {
		output.WriteString("## Built-in Skills\n\n")
		for _, skill := range bundledSkills {
			s.formatSkill(&output, skill)
		}
		output.WriteString("\n")
	}

	// Display file-based skills
	if len(fileSkills) > 0 {
		output.WriteString("## Custom Skills\n\n")
		for _, skill := range fileSkills {
			s.formatSkill(&output, skill)
		}
		output.WriteString("\n")
	}

	// Display other skills
	if len(otherSkills) > 0 {
		output.WriteString("## Other Skills\n\n")
		for _, skill := range otherSkills {
			s.formatSkill(&output, skill)
		}
		output.WriteString("\n")
	}

	// Summary
	stats := s.skillManager.GetStats()
	output.WriteString("---\n\n")
	output.WriteString(fmt.Sprintf("**Total:** %d skills | **Bundled:** %d | **Custom:** %d | **User-invocable:** %d\n\n",
		stats.TotalSkills, stats.BundledSkills, stats.DynamicSkills, stats.UserInvocable))
	output.WriteString("Type `/skillname` to use a skill, or `/skillname args` to pass arguments.\n")

	conn.WriteJSON(Message{
		Type:    "message",
		Role:    "assistant",
		Content: output.String(),
	})

	conn.WriteJSON(Message{Type: "done"})
}

func (s *Server) engineForScope(scope websandbox.Scope) *engine.Engine {
	if s.registryBuilder == nil {
		s.debugf("webui engineForScope using default engine working_dir=%s", scope.RootDir)
		return s.engine
	}
	s.debugf("webui engineForScope creating engine working_dir=%s skill=%s", scope.RootDir, scope.SkillName)
	client := anthropic.NewClient(s.apiKey, s.baseURL, 30*time.Second)
	planner := anthropic.NewPlanner(client, s.model)
	checker := permissions.NewChecker(permissions.ModeBypass, nil, nil)
	registry := s.registryBuilder(scope)
	eng := engine.NewWithDir(planner, registry, checker, 0, scope.RootDir)
	eng.SetSkillManager(s.skillManager)
	return eng
}

func (s *Server) sandboxOptions(scope websandbox.Scope) websandbox.RuntimeOptions {
	if s.sandboxFactory != nil {
		return s.sandboxFactory(scope)
	}
	return websandbox.RuntimeOptions{}
}

func effectiveSkillWorkingDir(skill *skills.SkillDefinition, fallback string) string {
	if skill != nil && strings.TrimSpace(skill.SkillRoot) != "" {
		return skill.SkillRoot
	}
	return fallback
}

func (s *Server) scopeForSkill(session *state.Session, skill *skills.SkillDefinition, skillDir string) websandbox.Scope {
	scope := websandbox.Scope{
		RootDir:      skillDir,
		SkillScoped:  true,
		AllowedTools: skill.AllowedTools,
		AllowedEnv:   skill.AllowedEnv,
		PrimaryEnv:   skill.PrimaryEnv,
	}
	if session != nil {
		scope.SessionID = session.ID
	}
	if skill != nil {
		scope.SkillName = skill.Name
	}
	return scope
}

func collectSkillScripts(skillDir string) []string {
	scriptsDir := filepath.Join(skillDir, "scripts")
	info, err := os.Stat(scriptsDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	var scripts []string
	_ = filepath.WalkDir(scriptsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(skillDir, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if scriptDepth(rel) > 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if scriptDepth(rel) > 3 {
			return nil
		}
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".py", ".sh":
			scripts = append(scripts, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(scripts)
	return scripts
}

func scriptDepth(rel string) int {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) == 1 && parts[0] == "." {
		return 0
	}
	return len(parts) - 1
}

func (s *Server) debugf(format string, args ...interface{}) {
	if s != nil && s.debug {
		log.Printf(format, args...)
	}
}

func isDebugEnabled() bool {
	value := strings.TrimSpace(os.Getenv("CLAUDE_GO_WEBUI_DEBUG"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || strings.EqualFold(value, "on")
}

// formatSkill formats a single skill for display
func (s *Server) formatSkill(output *strings.Builder, skill *skills.SkillDefinition) {
	// Skill name
	output.WriteString(fmt.Sprintf("### `/%s`", skill.Name))
	if skill.DisplayName != "" && skill.DisplayName != skill.Name {
		output.WriteString(fmt.Sprintf(" - %s", skill.DisplayName))
	}
	output.WriteString("\n\n")

	// Description
	if skill.Description != "" {
		// Truncate long descriptions
		desc := skill.Description
		if len(desc) > 150 {
			desc = desc[:147] + "..."
		}
		output.WriteString(fmt.Sprintf("%s\n\n", desc))
	}

	// Usage hint
	if skill.ArgumentHint != "" {
		output.WriteString(fmt.Sprintf("**Usage:** `/%s %s`\n\n", skill.Name, skill.ArgumentHint))
	} else if len(skill.ArgumentNames) > 0 {
		output.WriteString(fmt.Sprintf("**Usage:** `/%s %s`\n\n", skill.Name, strings.Join(skill.ArgumentNames, " ")))
	}

	// Source info (only for file-based skills, show path)
	if skill.Source == skills.SourceFile && skill.LoadedFrom != "" {
		// Show relative path if possible
		relPath := skill.LoadedFrom
		if strings.HasPrefix(relPath, s.workingDir) {
			relPath = strings.TrimPrefix(relPath, s.workingDir)
			relPath = strings.TrimPrefix(relPath, "/")
		}
		output.WriteString(fmt.Sprintf("*Source: %s*\n\n", relPath))
	}
}

func (s *Server) HandleStatic(w http.ResponseWriter, r *http.Request) {
	// Read from embedded file system
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		log.Printf("Error reading embedded index.html: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) Start(addr string) error {
	http.HandleFunc("/", s.HandleStatic)
	http.HandleFunc("/ws", s.HandleWebSocket)

	log.Printf("Web UI server starting on %s", addr)
	return http.ListenAndServe(addr, nil)
}

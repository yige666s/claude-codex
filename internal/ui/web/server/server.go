package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ding/claude-code/claude-go/internal/harness/anthropic"
	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	"github.com/ding/claude-code/claude-go/internal/harness/skills"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Server struct {
	engine       *engine.Engine
	skillManager *skills.SkillManager
	sessions     map[string]*state.Session
	mu           sync.RWMutex
	upgrader     websocket.Upgrader
	workingDir   string
}

type Message struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Role      string          `json:"role,omitempty"`
	Error     string          `json:"error,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func New(apiKey, baseURL, model string, registry *toolkit.Registry, skillManager *skills.SkillManager) *Server {
	client := anthropic.NewClient(apiKey, baseURL, 30*time.Second)
	planner := anthropic.NewPlanner(client, model)
	checker := permissions.NewChecker(permissions.ModeBypass, nil, nil)

	// Get working directory
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}

	eng := engine.NewWithDir(planner, registry, checker, 8, workingDir)
	eng.SetSkillManager(skillManager)

	return &Server{
		engine:       eng,
		skillManager: skillManager,
		sessions:     make(map[string]*state.Session),
		workingDir:   workingDir,
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

	session := state.NewSession(".")
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

	for {
		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("Read error: %v", err)
			break
		}

		if msg.Type == "chat" {
			s.handleChat(conn, sessionID, msg.Content)
		}
	}
}

func (s *Server) handleChat(conn *websocket.Conn, sessionID, prompt string) {
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

	// Run engine and stream response
	ctx := context.Background()
	result, err := s.engine.Run(ctx, session, prompt)
	if err != nil {
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
}

// handleSkillCommand handles skill invocations like /commit, /review, etc.
func (s *Server) handleSkillCommand(conn *websocket.Conn, session *state.Session, prompt string) {
	// Parse command: "/skillname args..."
	parts := strings.SplitN(strings.TrimSpace(prompt), " ", 2)
	skillName := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	// Special command: /skills - list all available skills
	if skillName == "skills" {
		s.handleListSkills(conn)
		return
	}

	// Get skill
	skill, ok := s.skillManager.GetSkill(skillName)
	if !ok {
		conn.WriteJSON(Message{
			Type:  "error",
			Error: fmt.Sprintf("Unknown skill: /%s. Use /skills to list available skills.", skillName),
		})
		conn.WriteJSON(Message{Type: "done"})
		return
	}

	// Check if user can invoke this skill
	if !skill.UserInvocable {
		conn.WriteJSON(Message{
			Type:  "error",
			Error: fmt.Sprintf("Skill /%s is not user-invocable.", skillName),
		})
		conn.WriteJSON(Message{Type: "done"})
		return
	}

	// Generate prompt from skill
	blocks, err := skill.GetPrompt(args, &skills.SkillContext{
		SessionID:  session.ID,
		WorkingDir: s.workingDir,
	})
	if err != nil {
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

	// Run engine with skill prompt
	ctx := context.Background()
	result, err := s.engine.Run(ctx, session, promptText)
	if err != nil {
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

func staticIndexPath() string {
	if _, filename, _, ok := runtime.Caller(0); ok {
		return filepath.Join(filepath.Dir(filename), "..", "static", "index.html")
	}
	return filepath.Join("internal", "ui", "web", "static", "index.html")
}

func (s *Server) HandleStatic(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, staticIndexPath())
}

func (s *Server) Start(addr string) error {
	http.HandleFunc("/", s.HandleStatic)
	http.HandleFunc("/ws", s.HandleWebSocket)

	log.Printf("Web UI server starting on %s", addr)
	return http.ListenAndServe(addr, nil)
}

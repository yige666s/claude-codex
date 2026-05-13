package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"claude-codex/internal/public/fsutil"
	publictypes "claude-codex/internal/public/types"
)

type Message struct {
	Role          string                     `json:"role"`
	Content       string                     `json:"content,omitempty"`
	ContentBlocks []publictypes.ContentBlock `json:"content_blocks,omitempty"`
	ToolName      string                     `json:"tool_name,omitempty"`
	ToolCallID    string                     `json:"tool_call_id,omitempty"` // Anthropic tool_use ID
	ToolInput     json.RawMessage            `json:"tool_input,omitempty"`
	ToolOutput    string                     `json:"tool_output,omitempty"`
	ToolCalls     []ToolCall                 `json:"tool_calls,omitempty"` // For assistant messages with tool_use
	CreatedAt     time.Time                  `json:"created_at"`
	Hidden        bool                       `json:"hidden,omitempty"` // hidden messages are not shown in the TUI
}

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type Session struct {
	ID          string            `json:"id"`
	WorkingDir  string            `json:"working_dir"`
	StartedAt   time.Time         `json:"started_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Usage       Usage             `json:"usage"`
	Messages    []Message         `json:"messages"`
	Tags        []string          `json:"tags,omitempty"`
	Description string            `json:"description,omitempty"`
	ParentID    string            `json:"parent_id,omitempty"`    // For branching
	BranchPoint int               `json:"branch_point,omitempty"` // Turn number where branch occurred
	Metadata    map[string]string `json:"metadata,omitempty"`
	Archived    bool              `json:"archived,omitempty"`
}

type Usage struct {
	InputChars       int     `json:"input_chars"`
	OutputChars      int     `json:"output_chars"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

func NewSession(workingDir string) *Session {
	now := time.Now().UTC()
	return &Session{
		ID:         newID(),
		WorkingDir: workingDir,
		StartedAt:  now,
		UpdatedAt:  now,
		Messages:   make([]Message, 0, 8),
	}
}

func (s *Session) AddUserMessage(content string) {
	s.append(Message{
		Role:      "user",
		Content:   content,
		CreatedAt: time.Now().UTC(),
	})
	s.Usage.RecordInput(content)
}

// AddSystemContext injects a hidden context message that is sent to the model
// but not displayed in the TUI transcript.
func (s *Session) AddSystemContext(content string) {
	s.append(Message{
		Role:      "user",
		Content:   content,
		Hidden:    true,
		CreatedAt: time.Now().UTC(),
	})
	s.Usage.RecordInput(content)
}

func (s *Session) AddAssistantMessage(content string) {
	s.append(Message{
		Role:      "assistant",
		Content:   content,
		CreatedAt: time.Now().UTC(),
	})
	s.Usage.RecordOutput(content)
}

func (s *Session) AddAssistantMessageWithTools(content string, toolCalls []ToolCall) {
	s.append(Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
		CreatedAt: time.Now().UTC(),
	})
	s.Usage.RecordOutput(content)
}

func (s *Session) AddToolResult(callID, toolName string, input json.RawMessage, output string) {
	s.append(Message{
		Role:       "tool",
		ToolCallID: callID,
		ToolName:   toolName,
		ToolInput:  input,
		ToolOutput: output,
		Hidden:     true,
		CreatedAt:  time.Now().UTC(),
	})
	s.Usage.RecordOutput(output)
}

func (s *Session) LastMessage() *Message {
	if len(s.Messages) == 0 {
		return nil
	}

	return &s.Messages[len(s.Messages)-1]
}

func (s *Session) LastUserMessage() string {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == "user" {
			return s.Messages[i].Content
		}
	}
	return ""
}

func (s *Session) Save(home string) (string, error) {
	path := filepath.Join(home, "sessions", s.ID+".json")
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}

	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return "", err
	}

	return path, nil
}

func LoadSession(home, id string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(home, "sessions", id+".json"))
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func LoadLatestSession(home string) (*Session, error) {
	dir := filepath.Join(home, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		name    string
		modTime time.Time
	}

	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate{
			name:    entry.Name(),
			modTime: info.ModTime(),
		})
	}

	if len(candidates) == 0 {
		return nil, errors.New("no saved sessions found")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	return LoadSession(home, trimSessionFilename(candidates[0].name))
}

func (s *Session) append(message Message) {
	s.Messages = append(s.Messages, message)
	s.UpdatedAt = message.CreatedAt
}

func (u *Usage) RecordInput(value string) {
	u.InputChars += len(value)
	u.InputTokens += estimateTokens(value)
	u.TotalTokens = u.InputTokens + u.OutputTokens
	u.EstimatedCostUSD = estimateCost(u.InputTokens, u.OutputTokens)
}

func (u *Usage) RecordOutput(value string) {
	u.OutputChars += len(value)
	u.OutputTokens += estimateTokens(value)
	u.TotalTokens = u.InputTokens + u.OutputTokens
	u.EstimatedCostUSD = estimateCost(u.InputTokens, u.OutputTokens)
}

func newID() string {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000Z")
	}

	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(buffer)
}

func estimateTokens(value string) int {
	if value == "" {
		return 0
	}

	tokens := len(value) / 4
	if len(value)%4 != 0 {
		tokens++
	}
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}

func estimateCost(inputTokens, outputTokens int) float64 {
	const (
		inputCostPerMillion  = 3.0
		outputCostPerMillion = 15.0
	)

	return (float64(inputTokens)*inputCostPerMillion + float64(outputTokens)*outputCostPerMillion) / 1_000_000.0
}

func trimSessionFilename(value string) string {
	return value[:len(value)-len(filepath.Ext(value))]
}

// AddTag adds a tag to the session
func (s *Session) AddTag(tag string) {
	for _, t := range s.Tags {
		if t == tag {
			return
		}
	}
	s.Tags = append(s.Tags, tag)
}

// RemoveTag removes a tag from the session
func (s *Session) RemoveTag(tag string) {
	for i, t := range s.Tags {
		if t == tag {
			s.Tags = append(s.Tags[:i], s.Tags[i+1:]...)
			return
		}
	}
}

// HasTag checks if session has a specific tag
func (s *Session) HasTag(tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// SetMetadata sets a metadata key-value pair
func (s *Session) SetMetadata(key, value string) {
	if s.Metadata == nil {
		s.Metadata = make(map[string]string)
	}
	s.Metadata[key] = value
}

// GetMetadata retrieves a metadata value
func (s *Session) GetMetadata(key string) (string, bool) {
	if s.Metadata == nil {
		return "", false
	}
	val, ok := s.Metadata[key]
	return val, ok
}

// Branch creates a new session branching from a specific turn
func (s *Session) Branch(turnNumber int, description string) (*Session, error) {
	if turnNumber < 0 || turnNumber >= len(s.Messages) {
		return nil, errors.New("invalid turn number")
	}

	now := time.Now().UTC()
	branch := &Session{
		ID:          newID(),
		WorkingDir:  s.WorkingDir,
		StartedAt:   now,
		UpdatedAt:   now,
		ParentID:    s.ID,
		BranchPoint: turnNumber,
		Description: description,
		Messages:    make([]Message, turnNumber+1),
		Tags:        append([]string{}, s.Tags...),
		Metadata:    make(map[string]string),
	}

	// Copy messages up to branch point
	copy(branch.Messages, s.Messages[:turnNumber+1])

	// Copy metadata
	for k, v := range s.Metadata {
		branch.Metadata[k] = v
	}

	// Recalculate usage
	for _, msg := range branch.Messages {
		if msg.Role == "user" {
			branch.Usage.RecordInput(msg.Content)
		} else if msg.Role == "assistant" {
			branch.Usage.RecordOutput(msg.Content)
		} else if msg.Role == "tool" {
			branch.Usage.RecordOutput(msg.ToolOutput)
		}
	}

	return branch, nil
}

// Export exports session to JSON
func (s *Session) Export() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// ImportSession imports a session from JSON
func ImportSession(data []byte) (*Session, error) {
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// Archive marks the session as archived
func (s *Session) Archive() {
	s.Archived = true
	s.UpdatedAt = time.Now().UTC()
}

// Unarchive marks the session as active
func (s *Session) Unarchive() {
	s.Archived = false
	s.UpdatedAt = time.Now().UTC()
}

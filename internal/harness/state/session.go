package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"

	"claude-codex/internal/public/fsutil"
	publictypes "claude-codex/internal/public/types"
)

const (
	SessionStatusActive   = 1
	SessionStatusArchived = 2
	SessionStatusDeleted  = 3
)

const (
	MessageRoleUser      = "user"
	MessageRoleAssistant = "assistant"
	MessageRoleSystem    = "system"
	MessageRoleTool      = "tool"
)

const (
	MessageContentTypeText       = "text"
	MessageContentTypeMultipart  = "multipart"
	MessageContentTypeToolCall   = "tool_call"
	MessageContentTypeToolResult = "tool_result"
	MessageContentTypeSummary    = "summary"
)

const (
	MessageStatusNormal    = 1
	MessageStatusDeleted   = 2
	MessageStatusTruncated = 3
)

type Message struct {
	ID               string                     `json:"id,omitempty"`
	SessionID        string                     `json:"session_id,omitempty"`
	UserID           string                     `json:"user_id,omitempty"`
	SeqNo            int64                      `json:"seq_no,omitempty"`
	ParentID         string                     `json:"parent_id,omitempty"`
	Role             string                     `json:"role"`
	ContentType      string                     `json:"content_type,omitempty"`
	Content          string                     `json:"content,omitempty"`
	ContentParts     []publictypes.ContentBlock `json:"content_parts,omitempty"`
	ContentBlocks    []publictypes.ContentBlock `json:"content_blocks,omitempty"`
	Attachments      []MessageAttachment        `json:"attachments,omitempty"`
	ToolName         string                     `json:"tool_name,omitempty"`
	ToolCallID       string                     `json:"tool_call_id,omitempty"` // Anthropic tool_use ID
	ToolInput        json.RawMessage            `json:"tool_input,omitempty"`
	ToolOutput       string                     `json:"tool_output,omitempty"`
	ToolCalls        []ToolCall                 `json:"tool_calls,omitempty"` // For assistant messages with tool_use
	PromptTokens     int                        `json:"prompt_tokens,omitempty"`
	CompletionTokens int                        `json:"completion_tokens,omitempty"`
	Status           int                        `json:"status,omitempty"`
	IsContextUsed    bool                       `json:"is_context_used,omitempty"`
	ModelID          string                     `json:"model_id,omitempty"`
	RunID            string                     `json:"run_id,omitempty"`
	ArchiveURI       string                     `json:"archive_uri,omitempty"`
	ArchiveChecksum  string                     `json:"archive_checksum,omitempty"`
	ArchivedAt       *time.Time                 `json:"archived_at,omitempty"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at,omitempty"`
	Hidden           bool                       `json:"hidden,omitempty"` // hidden messages are not shown in the TUI
}

type ToolCall struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Input            json.RawMessage `json:"input"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
}

type MessageAttachment struct {
	ID               string    `json:"id"`
	MessageID        string    `json:"message_id,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
	UserID           string    `json:"user_id,omitempty"`
	FileType         string    `json:"file_type"`
	MimeType         string    `json:"mime_type"`
	FileName         string    `json:"file_name,omitempty"`
	FileSize         int64     `json:"file_size,omitempty"`
	StorageKey       string    `json:"storage_key,omitempty"`
	ThumbnailKey     string    `json:"thumbnail_key,omitempty"`
	ExtractedTextKey string    `json:"extracted_text_key,omitempty"`
	EmbeddingStatus  int       `json:"embedding_status,omitempty"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
}

const (
	MessageAttachmentEmbeddingPending = 0
	MessageAttachmentEmbeddingDone    = 1
	MessageAttachmentEmbeddingFailed  = 2
)

type Session struct {
	ID            string            `json:"id"`
	UserID        string            `json:"user_id,omitempty"`
	AgentID       string            `json:"agent_id,omitempty"`
	Title         string            `json:"title,omitempty"`
	Status        int               `json:"status,omitempty"`
	WorkingDir    string            `json:"working_dir"`
	StartedAt     time.Time         `json:"started_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	LastMessageAt time.Time         `json:"last_message_at,omitempty"`
	Usage         Usage             `json:"usage"`
	Messages      []Message         `json:"messages"`
	MessageCount  int               `json:"message_count,omitempty"`
	TotalTokens   int64             `json:"total_tokens,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Description   string            `json:"description,omitempty"`
	ParentID      string            `json:"parent_id,omitempty"`    // For branching
	BranchPoint   int               `json:"branch_point,omitempty"` // Turn number where branch occurred
	Metadata      map[string]string `json:"metadata,omitempty"`
	Archived      bool              `json:"archived,omitempty"`
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
		Status:     SessionStatusActive,
		WorkingDir: workingDir,
		StartedAt:  now,
		UpdatedAt:  now,
		Messages:   make([]Message, 0, 8),
	}
}

func (s *Session) AddUserMessage(content string) {
	s.append(newTextMessage(MessageRoleUser, content, false))
	s.Usage.RecordInput(content)
}

func (s *Session) AddUserContentMessage(content string, blocks []publictypes.ContentBlock) {
	message := newTextMessage(MessageRoleUser, content, false)
	message.ContentType = MessageContentTypeMultipart
	message.ContentParts = append([]publictypes.ContentBlock(nil), blocks...)
	message.ContentBlocks = append([]publictypes.ContentBlock(nil), blocks...)
	s.append(message)
	s.Usage.RecordInput(content)
}

func (s *Session) AddHiddenUserMessage(content string) {
	s.append(newTextMessage(MessageRoleUser, content, true))
	s.Usage.RecordInput(content)
}

// AddSystemContext injects a hidden context message that is sent to the model
// but not displayed in the TUI transcript.
func (s *Session) AddSystemContext(content string) {
	s.append(newTextMessage(MessageRoleUser, content, true))
	s.Usage.RecordInput(content)
}

func (s *Session) AddAssistantMessage(content string) {
	s.append(newTextMessage(MessageRoleAssistant, content, false))
	s.Usage.RecordOutput(content)
}

func (s *Session) AddHiddenAssistantMessage(content string) {
	s.append(newTextMessage(MessageRoleAssistant, content, true))
	s.Usage.RecordOutput(content)
}

func (s *Session) AddAssistantMessageWithTools(content string, toolCalls []ToolCall) {
	message := newTextMessage(MessageRoleAssistant, content, false)
	message.ContentType = MessageContentTypeToolCall
	message.ToolCalls = toolCalls
	s.append(message)
	s.Usage.RecordOutput(content)
}

func (s *Session) AddToolResult(callID, toolName string, input json.RawMessage, output string) {
	now := time.Now().UTC()
	s.append(Message{
		ID:            newID(),
		Role:          "tool",
		ContentType:   MessageContentTypeToolResult,
		ToolCallID:    callID,
		ToolName:      toolName,
		ToolInput:     input,
		ToolOutput:    output,
		Status:        MessageStatusNormal,
		IsContextUsed: true,
		Hidden:        true,
		CreatedAt:     now,
		UpdatedAt:     now,
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
	if message.ID == "" {
		message.ID = newID()
	}
	if message.Status == 0 {
		message.Status = MessageStatusNormal
	}
	if message.ContentType == "" {
		message.ContentType = inferMessageContentType(message)
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.CreatedAt
	}
	message.SeqNo = int64(len(s.Messages) + 1)
	message.SessionID = s.ID
	message.UserID = s.UserID
	message.IsContextUsed = true
	s.Messages = append(s.Messages, message)
	s.UpdatedAt = message.CreatedAt
	s.LastMessageAt = message.CreatedAt
	s.MessageCount = len(s.Messages)
	s.TotalTokens = int64(s.Usage.TotalTokens)
}

func newTextMessage(role, content string, hidden bool) Message {
	now := time.Now().UTC()
	return Message{
		ID:            newID(),
		Role:          role,
		ContentType:   MessageContentTypeText,
		Content:       content,
		Status:        MessageStatusNormal,
		IsContextUsed: true,
		Hidden:        hidden,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func inferMessageContentType(message Message) string {
	if len(message.ContentParts) > 0 || len(message.ContentBlocks) > 0 {
		return MessageContentTypeMultipart
	}
	if message.Role == MessageRoleTool || message.ToolCallID != "" || message.ToolOutput != "" {
		return MessageContentTypeToolResult
	}
	if len(message.ToolCalls) > 0 {
		return MessageContentTypeToolCall
	}
	return MessageContentTypeText
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
	return uuid.NewString()
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

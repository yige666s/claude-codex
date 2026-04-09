package types

import (
	"encoding/json"
	"sync"
	"time"
)

// AppState represents the global application state.
type AppState struct {
	mu sync.RWMutex

	// Session information
	SessionID      string            `json:"session_id"`
	WorkingDir     string            `json:"working_dir"`
	StartedAt      time.Time         `json:"started_at"`
	UpdatedAt      time.Time         `json:"updated_at"`

	// Conversation state
	Messages       []Message         `json:"messages"`
	Usage          Usage             `json:"usage"`

	// Session metadata
	Tags           []string          `json:"tags,omitempty"`
	Description    string            `json:"description,omitempty"`
	ParentID       string            `json:"parent_id,omitempty"`
	BranchPoint    int               `json:"branch_point,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Archived       bool              `json:"archived,omitempty"`

	// File state cache
	FileStateCache *FileStateCache   `json:"file_state_cache,omitempty"`

	// SDK status
	SDKStatus      *SDKStatus        `json:"sdk_status,omitempty"`
}

// FileStateCache tracks files that have been read during the session.
type FileStateCache struct {
	mu    sync.RWMutex
	Files map[string]*FileState `json:"files"`
}

// FileState represents the state of a file at a point in time.
type FileState struct {
	Path         string    `json:"path"`
	Hash         string    `json:"hash"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	ReadAt       time.Time `json:"read_at"`
	ReadCount    int       `json:"read_count"`
	LastModified time.Time `json:"last_modified"`
}

// Usage represents token and cost usage information.
type Usage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	TotalTokens              int     `json:"total_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens,omitempty"`
	EstimatedCostUSD         float64 `json:"estimated_cost_usd"`
	InputChars               int     `json:"input_chars"`
	OutputChars              int     `json:"output_chars"`
}

// NonNullableUsage is a variant of Usage that ensures all fields have values.
type NonNullableUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	TotalTokens              int     `json:"total_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	EstimatedCostUSD         float64 `json:"estimated_cost_usd"`
}

// SDKStatus represents the current status of the SDK session.
type SDKStatus struct {
	Status  string `json:"status"`  // "idle", "running", "error", "completed"
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewAppState creates a new application state.
func NewAppState(sessionID, workingDir string) *AppState {
	now := time.Now().UTC()
	return &AppState{
		SessionID:      sessionID,
		WorkingDir:     workingDir,
		StartedAt:      now,
		UpdatedAt:      now,
		Messages:       make([]Message, 0),
		Metadata:       make(map[string]string),
		FileStateCache: NewFileStateCache(),
	}
}

// NewFileStateCache creates a new file state cache.
func NewFileStateCache() *FileStateCache {
	return &FileStateCache{
		Files: make(map[string]*FileState),
	}
}

// AddMessage adds a message to the state.
func (s *AppState) AddMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now().UTC()
}

// GetMessages returns a copy of all messages.
func (s *AppState) GetMessages() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages := make([]Message, len(s.Messages))
	copy(messages, s.Messages)
	return messages
}

// UpdateUsage updates the usage statistics.
func (s *AppState) UpdateUsage(usage Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Usage.InputTokens += usage.InputTokens
	s.Usage.OutputTokens += usage.OutputTokens
	s.Usage.TotalTokens += usage.TotalTokens
	s.Usage.CacheCreationInputTokens += usage.CacheCreationInputTokens
	s.Usage.CacheReadInputTokens += usage.CacheReadInputTokens
	s.Usage.EstimatedCostUSD += usage.EstimatedCostUSD
	s.Usage.InputChars += usage.InputChars
	s.Usage.OutputChars += usage.OutputChars
}

// GetUsage returns a copy of the current usage.
func (s *AppState) GetUsage() Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Usage
}

// SetMetadata sets a metadata key-value pair.
func (s *AppState) SetMetadata(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Metadata == nil {
		s.Metadata = make(map[string]string)
	}
	s.Metadata[key] = value
	s.UpdatedAt = time.Now().UTC()
}

// GetMetadata retrieves a metadata value.
func (s *AppState) GetMetadata(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, ok := s.Metadata[key]
	return val, ok
}

// AddTag adds a tag to the session.
func (s *AppState) AddTag(tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.Tags {
		if t == tag {
			return
		}
	}
	s.Tags = append(s.Tags, tag)
	s.UpdatedAt = time.Now().UTC()
}

// RecordFileRead records that a file was read.
func (fsc *FileStateCache) RecordFileRead(path, hash string, size int64, modTime time.Time) {
	fsc.mu.Lock()
	defer fsc.mu.Unlock()

	now := time.Now().UTC()
	if state, exists := fsc.Files[path]; exists {
		state.ReadCount++
		state.ReadAt = now
		state.Hash = hash
		state.Size = size
		state.ModTime = modTime
	} else {
		fsc.Files[path] = &FileState{
			Path:         path,
			Hash:         hash,
			Size:         size,
			ModTime:      modTime,
			ReadAt:       now,
			ReadCount:    1,
			LastModified: modTime,
		}
	}
}

// GetFileState retrieves the state of a file.
func (fsc *FileStateCache) GetFileState(path string) (*FileState, bool) {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	state, exists := fsc.Files[path]
	return state, exists
}

// HasFileChanged checks if a file has changed since it was last read.
func (fsc *FileStateCache) HasFileChanged(path, hash string) bool {
	fsc.mu.RLock()
	defer fsc.mu.RUnlock()

	state, exists := fsc.Files[path]
	if !exists {
		return true
	}
	return state.Hash != hash
}

// Export exports the state to JSON.
func (s *AppState) Export() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return json.MarshalIndent(s, "", "  ")
}

// Import imports state from JSON.
func (s *AppState) Import(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return json.Unmarshal(data, s)
}

// AccumulateUsage adds usage from one Usage to another.
func AccumulateUsage(total, current Usage) Usage {
	return Usage{
		InputTokens:              total.InputTokens + current.InputTokens,
		OutputTokens:             total.OutputTokens + current.OutputTokens,
		TotalTokens:              total.TotalTokens + current.TotalTokens,
		CacheCreationInputTokens: total.CacheCreationInputTokens + current.CacheCreationInputTokens,
		CacheReadInputTokens:     total.CacheReadInputTokens + current.CacheReadInputTokens,
		EstimatedCostUSD:         total.EstimatedCostUSD + current.EstimatedCostUSD,
		InputChars:               total.InputChars + current.InputChars,
		OutputChars:              total.OutputChars + current.OutputChars,
	}
}

// ToNonNullable converts Usage to NonNullableUsage.
func (u Usage) ToNonNullable() NonNullableUsage {
	return NonNullableUsage{
		InputTokens:              u.InputTokens,
		OutputTokens:             u.OutputTokens,
		TotalTokens:              u.TotalTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		EstimatedCostUSD:         u.EstimatedCostUSD,
	}
}

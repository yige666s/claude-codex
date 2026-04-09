package types

import "time"

// HookType represents the type of hook.
type HookType string

const (
	HookTypePreToolUse   HookType = "PreToolUse"
	HookTypePostToolUse  HookType = "PostToolUse"
	HookTypePreQuery     HookType = "PreQuery"
	HookTypePostQuery    HookType = "PostQuery"
	HookTypePreSubmit    HookType = "PreSubmit"
	HookTypePostSubmit   HookType = "PostSubmit"
	HookTypeOnError      HookType = "OnError"
	HookTypeOnComplete   HookType = "OnComplete"
	HookTypeSubagentStart HookType = "SubagentStart"
)

// HookProgress represents progress information from a hook.
type HookProgress struct {
	Type      HookType               `json:"type"`
	ToolName  string                 `json:"tool_name,omitempty"`
	Status    string                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Progress  float64                `json:"progress,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// PromptRequest represents a request for user input via prompt.
type PromptRequest struct {
	Type        string                 `json:"type"`        // "text", "confirm", "select", "multiselect"
	Message     string                 `json:"message"`
	Default     interface{}            `json:"default,omitempty"`
	Choices     []string               `json:"choices,omitempty"`
	Placeholder string                 `json:"placeholder,omitempty"`
	Validate    func(string) error     `json:"-"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PromptResponse represents a response to a prompt request.
type PromptResponse struct {
	Value    interface{} `json:"value"`
	Canceled bool        `json:"canceled"`
	Error    string      `json:"error,omitempty"`
}

// ElicitRequestURLParams represents URL parameters for an elicit request.
type ElicitRequestURLParams struct {
	URL         string            `json:"url"`
	Method      string            `json:"method,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
	FollowRedirects bool          `json:"follow_redirects,omitempty"`
}

// ElicitResult represents the result of an elicit request.
type ElicitResult struct {
	Success    bool                   `json:"success"`
	StatusCode int                    `json:"status_code,omitempty"`
	Headers    map[string]string      `json:"headers,omitempty"`
	Body       string                 `json:"body,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// HookConfig represents configuration for a hook.
type HookConfig struct {
	Type     HookType `json:"type"`
	Enabled  bool     `json:"enabled"`
	Command  string   `json:"command,omitempty"`
	Script   string   `json:"script,omitempty"`
	Timeout  int      `json:"timeout,omitempty"` // Timeout in seconds
	Async    bool     `json:"async,omitempty"`   // Run asynchronously
}

// HookResult represents the result of executing a hook.
type HookResult struct {
	Success   bool                   `json:"success"`
	Output    string                 `json:"output,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Modified  bool                   `json:"modified,omitempty"`  // Whether the hook modified data
	Continue  bool                   `json:"continue"`            // Whether to continue execution
}

// HookContext provides context for hook execution.
type HookContext struct {
	SessionID  string                 `json:"session_id"`
	WorkingDir string                 `json:"working_dir"`
	ToolName   string                 `json:"tool_name,omitempty"`
	Input      map[string]interface{} `json:"input,omitempty"`
	Output     interface{}            `json:"output,omitempty"`
	Error      error                  `json:"-"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// NewHookProgress creates a new hook progress event.
func NewHookProgress(hookType HookType, status, message string) *HookProgress {
	return &HookProgress{
		Type:      hookType,
		Status:    status,
		Message:   message,
		Timestamp: time.Now().UTC(),
		Metadata:  make(map[string]interface{}),
	}
}

// NewHookResult creates a new hook result.
func NewHookResult(success bool, output string) *HookResult {
	return &HookResult{
		Success:  success,
		Output:   output,
		Continue: true,
		Metadata: make(map[string]interface{}),
	}
}

// WithError adds an error to the hook result.
func (hr *HookResult) WithError(err string) *HookResult {
	hr.Success = false
	hr.Error = err
	return hr
}

// WithMetadata adds metadata to the hook result.
func (hr *HookResult) WithMetadata(key string, value interface{}) *HookResult {
	if hr.Metadata == nil {
		hr.Metadata = make(map[string]interface{})
	}
	hr.Metadata[key] = value
	return hr
}

// StopExecution marks the hook result to stop execution.
func (hr *HookResult) StopExecution() *HookResult {
	hr.Continue = false
	return hr
}

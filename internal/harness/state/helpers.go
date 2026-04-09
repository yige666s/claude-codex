package state

// Helper methods for AppState
// Note: These methods do NOT lock the mutex. They are meant to be used either:
// 1. Through Store.SetState() which handles locking at the Store level
// 2. Directly on AppState instances in single-threaded contexts (e.g., tests)

// GetTask retrieves a task by ID
func (s *AppState) GetTask(taskID string) (interface{}, bool) {
	task, ok := s.Tasks[taskID]
	return task, ok
}

// AddTask adds a task to the state
func (s *AppState) AddTask(taskID string, task interface{}) {
	// Type assertion would be needed here in real implementation
	// s.Tasks[taskID] = task.(tasks.TaskState)
}

// RemoveTask removes a task from the state
func (s *AppState) RemoveTask(taskID string) {
	delete(s.Tasks, taskID)
}

// RegisterAgentName registers an agent name to ID mapping
func (s *AppState) RegisterAgentName(name string, agentID string) {
	s.AgentNameRegistry[name] = agentID
}

// GetAgentIDByName retrieves an agent ID by name
func (s *AppState) GetAgentIDByName(name string) (string, bool) {
	agentID, ok := s.AgentNameRegistry[name]
	return agentID, ok
}

// AddNotification adds a notification to the queue
func (s *AppState) AddNotification(notification Notification) {
	s.Notifications.Queue = append(s.Notifications.Queue, notification)
}

// GetCurrentNotification returns the current notification
func (s *AppState) GetCurrentNotification() *Notification {
	return s.Notifications.Current
}

// SetCurrentNotification sets the current notification
func (s *AppState) SetCurrentNotification(notification *Notification) {
	s.Notifications.Current = notification
}

// AddFileSnapshot adds a file snapshot to history
func (s *AppState) AddFileSnapshot(snapshot FileSnapshot) {
	s.FileHistory.Snapshots = append(s.FileHistory.Snapshots, snapshot)
	s.FileHistory.SnapshotSequence++
}

// TrackFile marks a file as tracked
func (s *AppState) TrackFile(path string) {
	s.FileHistory.TrackedFiles[path] = true
}

// IsFileTracked checks if a file is tracked
func (s *AppState) IsFileTracked(path string) bool {
	return s.FileHistory.TrackedFiles[path]
}

// AddMCPTool adds an MCP tool
func (s *AppState) AddMCPTool(tool MCPTool) {
	s.MCP.Tools = append(s.MCP.Tools, tool)
}

// GetMCPTools returns all MCP tools
func (s *AppState) GetMCPTools() []MCPTool {
	// Return a copy to prevent external modification
	tools := make([]MCPTool, len(s.MCP.Tools))
	copy(tools, s.MCP.Tools)
	return tools
}

// AddPlugin adds a plugin to enabled list
func (s *AppState) AddPlugin(plugin LoadedPlugin) {
	s.Plugins.Enabled = append(s.Plugins.Enabled, plugin)
}

// GetEnabledPlugins returns all enabled plugins
func (s *AppState) GetEnabledPlugins() []LoadedPlugin {
	plugins := make([]LoadedPlugin, len(s.Plugins.Enabled))
	copy(plugins, s.Plugins.Enabled)
	return plugins
}

// SetPermissionMode sets the permission mode
func (s *AppState) SetPermissionMode(mode string) {
	s.ToolPermissionContext.Mode = mode
}

// GetPermissionMode returns the current permission mode
func (s *AppState) GetPermissionMode() string {
	return s.ToolPermissionContext.Mode
}

// SetVerbose sets verbose mode
func (s *AppState) SetVerbose(verbose bool) {
	s.Verbose = verbose
}

// IsVerbose returns whether verbose mode is enabled
func (s *AppState) IsVerbose() bool {
	return s.Verbose
}

// SetMainLoopModel sets the main loop model
func (s *AppState) SetMainLoopModel(model *string) {
	s.MainLoopModel = model
}

// GetMainLoopModel returns the main loop model
func (s *AppState) GetMainLoopModel() *string {
	return s.MainLoopModel
}

// IncrementAuthVersion increments the auth version
func (s *AppState) IncrementAuthVersion() {
	s.AuthVersion++
}

// GetAuthVersion returns the current auth version
func (s *AppState) GetAuthVersion() int {
	return s.AuthVersion
}

// Package types provides common types used across the Claude Code core packages.
//
// This package contains shared type definitions for messages, state, configuration,
// permissions, tools, hooks, notifications, and other core concepts used throughout
// the Claude Code system.
//
// Key type categories:
//
//   - Messages: Message types and content blocks for conversation management
//   - State: Application and session state management
//   - Configuration: System and session configuration types
//   - Permissions: Permission checking and authorization types
//   - Tools: Tool execution and registry types
//   - Hooks: Hook execution and progress reporting types
//   - Notifications: Notification system types
//   - Events: Event bus and event handling types
//   - Streaming: Streaming API event types
//   - Errors: Structured error types with context
//
// Usage:
//
//	import "claude-codex/internal/public/types"
//
//	// Create a new user message
//	msg := types.NewUserMessage("Hello, Claude!")
//
//	// Create application state
//	state := types.NewAppState(sessionID, workingDir)
//
//	// Check permissions
//	result := types.AllowPermission("User approved")
package types

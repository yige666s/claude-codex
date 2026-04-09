package tasks

import (
	"crypto/rand"
	"fmt"
)

// Task ID prefixes for different task types
var taskIDPrefixes = map[TaskType]string{
	TaskTypeLocalBash:         "b", // Keep as 'b' for backward compatibility
	TaskTypeLocalAgent:        "a",
	TaskTypeRemoteAgent:       "r",
	TaskTypeInProcessTeammate: "t",
	TaskTypeLocalWorkflow:     "w",
	TaskTypeMonitorMCP:        "m",
	TaskTypeDream:             "d",
}

// Case-insensitive-safe alphabet (digits + lowercase) for task IDs.
// 36^8 ≈ 2.8 trillion combinations, sufficient to resist brute-force symlink attacks.
const taskIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// GetTaskIDPrefix returns the prefix for a task type
func GetTaskIDPrefix(taskType TaskType) string {
	if prefix, ok := taskIDPrefixes[taskType]; ok {
		return prefix
	}
	return "x"
}

// GenerateTaskID generates a unique task ID for the given task type
func GenerateTaskID(taskType TaskType) (string, error) {
	prefix := GetTaskIDPrefix(taskType)
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	id := prefix
	for i := 0; i < 8; i++ {
		id += string(taskIDAlphabet[int(bytes[i])%len(taskIDAlphabet)])
	}
	return id, nil
}

// GenerateMainSessionTaskID generates a task ID for main session tasks
// Uses 's' prefix to distinguish from agent tasks ('a' prefix)
func GenerateMainSessionTaskID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	id := "s"
	for i := 0; i < 8; i++ {
		id += string(taskIDAlphabet[int(bytes[i])%len(taskIDAlphabet)])
	}
	return id, nil
}

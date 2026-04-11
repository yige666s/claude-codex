// Package sendmessage implements the SendMessage tool for agent-to-agent messaging.
//
// SendMessage routes a message to a named or ID-addressed agent using a
// file-based mailbox system. When the target agent is running in the same
// process the message is queued for delivery on its next tool-loop iteration.
// If the target is identified only by name or ID the message is written to a
// mailbox file on disk so a resumed agent can pick it up.
//
// UDS and cross-machine bridge routing from the TypeScript implementation are
// out of scope here — those depend on the networking/OAuth infrastructure that
// has not been ported yet.
package sendmessage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

// Tool implements the SendMessage tool.
type Tool struct {
	// MailboxDir is the directory where per-agent mailbox files are written.
	// Defaults to $HOME/.claude/mailboxes when empty.
	MailboxDir string

	// FindRunningAgent looks up a running in-process agent by name or ID and
	// returns a channel to deliver messages. Returns nil when the agent is not
	// running in-process.
	FindRunningAgent func(nameOrID string) chan<- string
}

// sendInput is the JSON input schema for SendMessage.
type sendInput struct {
	// To is the recipient: an agent ID (e.g. "agent-a1b"), a registered name,
	// or "*" for broadcast.
	To string `json:"to"`
	// Message is the text content to deliver.
	Message string `json:"message"`
	// Summary is an optional human-readable summary (used for UI display).
	Summary string `json:"summary,omitempty"`
}

// MailboxEntry is the format written to a per-agent mailbox file.
type MailboxEntry struct {
	From      string `json:"from"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

func (t *Tool) Name() string { return "SendMessage" }

func (t *Tool) Description() string {
	return `Send a message to another agent by its ID or registered name.

Use this to:
- Continue a background agent with follow-up instructions
- Broadcast instructions to all team members (to="*")

The recipient receives the message on its next iteration.`
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "to": {
      "type": "string",
      "description": "Agent ID, registered agent name, or \"*\" for broadcast."
    },
    "message": {
      "type": "string",
      "description": "The message content to send."
    },
    "summary": {
      "type": "string",
      "description": "Optional human-readable summary for UI display."
    }
  },
  "required": ["to", "message"]
}`)
}

func (t *Tool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *Tool) IsConcurrencySafe() bool       { return false }

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in sendInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("SendMessage: invalid input: %w", err)
	}
	if strings.TrimSpace(in.To) == "" {
		return toolkit.Result{}, fmt.Errorf("SendMessage: 'to' field is required")
	}
	if strings.TrimSpace(in.Message) == "" {
		return toolkit.Result{}, fmt.Errorf("SendMessage: 'message' field is required")
	}

	// Broadcast to all agents via mailbox scan.
	if in.To == "*" {
		n, err := t.broadcast(in.Message)
		if err != nil {
			return toolkit.Result{}, fmt.Errorf("SendMessage broadcast: %w", err)
		}
		return toolkit.Result{Output: fmt.Sprintf("Broadcast sent to %d agents.", n)}, nil
	}

	// Try in-process delivery first.
	if t.FindRunningAgent != nil {
		if ch := t.FindRunningAgent(in.To); ch != nil {
			select {
			case ch <- in.Message:
				return toolkit.Result{Output: fmt.Sprintf("Message queued for agent %s.", in.To)}, nil
			case <-ctx.Done():
				return toolkit.Result{}, ctx.Err()
			default:
				// Channel full — fall through to mailbox.
			}
		}
	}

	// Write to mailbox file for the target agent.
	if err := t.writeMailbox(in.To, in.Message); err != nil {
		return toolkit.Result{}, fmt.Errorf("SendMessage: failed to write mailbox: %w", err)
	}
	return toolkit.Result{Output: fmt.Sprintf("Message written to mailbox for agent %s.", in.To)}, nil
}

// mailboxDir returns the effective mailbox directory.
func (t *Tool) mailboxDir() string {
	if t.MailboxDir != "" {
		return t.MailboxDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "mailboxes")
}

// writeMailbox appends a message entry to the agent's mailbox file.
func (t *Tool) writeMailbox(agentID, message string) error {
	dir := t.mailboxDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Sanitize agentID for use as filename.
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, agentID)

	path := filepath.Join(dir, safe+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := MailboxEntry{
		From:      "coordinator",
		Message:   message,
		Timestamp: time.Now().UnixMilli(),
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", b)
	return err
}

// broadcast writes a message to all existing mailbox files.
func (t *Tool) broadcast(message string) (int, error) {
	dir := t.mailboxDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		agentID := strings.TrimSuffix(e.Name(), ".jsonl")
		if err := t.writeMailbox(agentID, message); err != nil {
			continue // best-effort broadcast
		}
		count++
	}
	return count, nil
}

// ReadMailbox reads and drains all pending messages for the given agent ID.
// Returns messages in arrival order. The mailbox file is truncated after reading.
func ReadMailbox(mailboxDir, agentID string) ([]MailboxEntry, error) {
	if mailboxDir == "" {
		home, _ := os.UserHomeDir()
		mailboxDir = filepath.Join(home, ".claude", "mailboxes")
	}

	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, agentID)

	path := filepath.Join(mailboxDir, safe+".jsonl")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Truncate the mailbox (drain semantics).
	_ = os.Truncate(path, 0)

	var entries []MailboxEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry MailboxEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

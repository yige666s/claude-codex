# Session Storage System

Go implementation of the Claude Code session storage system, migrated from TypeScript.

## Overview

This package provides a robust, concurrent-safe session storage system using JSONL (JSON Lines) format for transcript persistence. It supports:

- **Transcript Recording**: Append-only log of conversation messages
- **Session Metadata**: Titles, tags, agent info, PR links
- **Incremental Writes**: Batched writes with configurable flush intervals
- **Snapshot Management**: Point-in-time session snapshots
- **Concurrent Safety**: Thread-safe operations with proper locking

## Architecture

### Core Components

1. **TranscriptWriter**: Handles writing entries to JSONL files
   - Batched writes for performance
   - Configurable flush intervals (default: 100ms)
   - Automatic directory creation
   - Concurrent-safe operations

2. **TranscriptReader**: Reads entries from JSONL files
   - Full transcript loading
   - Tail reading for metadata
   - Robust error handling

3. **SessionStorage**: High-level session management
   - Message recording
   - Metadata management
   - Transcript loading
   - Integration with existing state.Session

4. **SnapshotManager**: Snapshot creation and management
   - Point-in-time snapshots
   - Snapshot compaction
   - Restore from snapshot

## File Format

### JSONL Structure

Each line in the transcript file is a JSON object representing an entry:

```jsonl
{"type":"user","uuid":"msg-001","role":"user","content":"Hello","sessionId":"session-123","timestamp":"2024-01-01T12:00:00Z"}
{"type":"assistant","uuid":"msg-002","role":"assistant","content":"Hi there!","sessionId":"session-123","timestamp":"2024-01-01T12:00:01Z"}
{"type":"custom-title","customTitle":"My Session","sessionId":"session-123","timestamp":"2024-01-01T12:00:02Z"}
```

### Entry Types

- **Transcript Messages**: `user`, `assistant`, `attachment`, `system`, `tool`
- **Metadata**: `custom-title`, `ai-title`, `last-prompt`, `tag`, `agent-name`, `agent-color`, `agent-setting`, `mode`, `task-summary`
- **Links**: `pr-link`
- **State**: `worktree-state`
- **Snapshots**: `file-history-snapshot`, `attribution-snapshot`, `content-replacement`
- **Context**: `context-collapse-commit`, `context-collapse-snapshot`

## Usage

### Basic Session Recording

```go
import "github.com/ding/claude-code/claude-go/internal/harness/storage"

// Create session storage
store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
if err != nil {
    log.Fatal(err)
}
defer store.Close()

// Record a message
msg := &storage.TranscriptMessage{
    UUID:    "msg-001",
    Role:    "user",
    Content: "Hello, world!",
}
store.RecordMessage(msg)

// Set metadata
store.SetCustomTitle("My Session")
store.SetTag("important")

// Flush to disk
store.Flush()
```

### Loading Existing Session

```go
// Load transcript
entries, err := store.LoadTranscript()
if err != nil {
    log.Fatal(err)
}

// Get metadata
meta := store.GetMetadata()
fmt.Printf("Title: %s\n", meta.CustomTitle)
fmt.Printf("Tag: %s\n", meta.Tag)
```

### Creating Snapshots

```go
snapshotMgr := storage.NewSnapshotManager(homeDir)

// Create snapshot from entries
entries, _ := store.LoadTranscript()
snapshot, err := snapshotMgr.CreateSnapshot(sessionID, entries)

// Save snapshot
path, err := snapshotMgr.SaveSnapshot(snapshot)

// Load snapshot
loaded, err := snapshotMgr.LoadSnapshot(path)

// Compact snapshot (keep last N messages)
compacted := snapshotMgr.CompactSnapshot(snapshot, 50)
```

### Concurrent Writes

The storage system is thread-safe and supports concurrent writes:

```go
// Multiple goroutines can safely write
for i := 0; i < 10; i++ {
    go func(id int) {
        msg := &storage.TranscriptMessage{
            UUID:    fmt.Sprintf("msg-%d", id),
            Role:    "user",
            Content: "Concurrent message",
        }
        store.RecordMessage(msg)
    }(i)
}

// Flush ensures all writes complete
store.Flush()
```

## Configuration

### Constants

```go
const (
    FlushIntervalMS = 100 * time.Millisecond  // Batch write interval
    MaxChunkBytes   = 100 * 1024 * 1024       // 100MB max batch size
    LiteReadBufSize = 64 * 1024               // 64KB tail read buffer
)
```

### Customization

The flush interval and chunk size can be adjusted by modifying the constants or by creating custom writer instances.

## Performance Characteristics

### Write Performance

- **Batched Writes**: Multiple entries are batched together and written in a single I/O operation
- **Flush Interval**: Default 100ms provides good balance between latency and throughput
- **Concurrent Safety**: Lock-free queue with periodic draining

### Read Performance

- **Full Load**: O(n) where n is the number of entries
- **Tail Read**: O(1) for metadata access (reads last 64KB)
- **Streaming**: Scanner-based reading for memory efficiency

## Error Handling

The storage system provides robust error handling:

- **Directory Creation**: Automatically creates missing directories
- **File Permissions**: Sets secure permissions (0600 for files, 0700 for directories)
- **Parse Errors**: Continues reading on malformed lines
- **Concurrent Access**: Thread-safe with proper locking

## Integration with Existing Code

### With state.Session

```go
import (
    "github.com/ding/claude-code/claude-go/internal/harness/state"
    "github.com/ding/claude-code/claude-go/internal/harness/storage"
)

// Create session
session := state.NewSession(workingDir)

// Create storage
store, _ := storage.NewSessionStorage(homeDir, session.ID, projectDir)

// Record session messages
for _, msg := range session.Messages {
    transcriptMsg := &storage.TranscriptMessage{
        UUID:    generateUUID(),
        Role:    msg.Role,
        Content: msg.Content,
    }
    store.RecordMessage(transcriptMsg)
}
```

## Testing

Run the test suite:

```bash
go test ./internal/harness/storage/...
```

Run with coverage:

```bash
go test -cover ./internal/harness/storage/...
```

Run specific tests:

```bash
go test -v -run TestTranscriptWriter ./internal/harness/storage/...
```

## Migration Notes

### From TypeScript

Key differences from the TypeScript implementation:

1. **Type System**: Go's static typing provides compile-time safety
2. **Concurrency**: Go's goroutines and channels replace Promise-based async
3. **Error Handling**: Explicit error returns instead of try-catch
4. **Memory Management**: Go's GC handles cleanup automatically

### Compatibility

The JSONL format is fully compatible with the TypeScript implementation, allowing:

- Reading TypeScript-generated transcripts in Go
- Reading Go-generated transcripts in TypeScript
- Seamless migration between implementations

## Future Enhancements

Potential improvements:

- [ ] Compression support for large transcripts
- [ ] Incremental loading for very large files
- [ ] Index files for faster seeking
- [ ] Encryption support for sensitive data
- [ ] Automatic cleanup of old snapshots
- [ ] Metrics and monitoring hooks

## License

Part of the Claude Code project.

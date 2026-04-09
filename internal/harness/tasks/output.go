package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// MaxTaskOutputBytes is the disk cap for task output files
	MaxTaskOutputBytes        = 5 * 1024 * 1024 * 1024 // 5GB
	MaxTaskOutputBytesDisplay = "5GB"
	DefaultMaxReadBytes       = 8 * 1024 * 1024 // 8MB
)

var (
	// taskOutputDir is memoized to prevent path changes after /clear
	taskOutputDir   string
	taskOutputMutex sync.RWMutex
)

// GetTaskOutputDir returns the task output directory for this session
// The session ID is captured at FIRST CALL to prevent path changes after /clear
func GetTaskOutputDir(projectTempDir, sessionID string) string {
	taskOutputMutex.RLock()
	if taskOutputDir != "" {
		defer taskOutputMutex.RUnlock()
		return taskOutputDir
	}
	taskOutputMutex.RUnlock()

	taskOutputMutex.Lock()
	defer taskOutputMutex.Unlock()

	// Double-check after acquiring write lock
	if taskOutputDir != "" {
		return taskOutputDir
	}

	taskOutputDir = filepath.Join(projectTempDir, sessionID, "tasks")
	return taskOutputDir
}

// ResetTaskOutputDirForTest clears the memoized directory (test helper)
func ResetTaskOutputDirForTest() {
	taskOutputMutex.Lock()
	defer taskOutputMutex.Unlock()
	taskOutputDir = ""
}

// GetTaskOutputPath returns the output file path for a task
func GetTaskOutputPath(projectTempDir, sessionID, taskID string) string {
	dir := GetTaskOutputDir(projectTempDir, sessionID)
	return filepath.Join(dir, fmt.Sprintf("%s.output", taskID))
}

// EnsureOutputDir ensures the task output directory exists
func EnsureOutputDir(projectTempDir, sessionID string) error {
	dir := GetTaskOutputDir(projectTempDir, sessionID)
	return os.MkdirAll(dir, 0755)
}

// DiskTaskOutput encapsulates async disk writes for a single task's output
type DiskTaskOutput struct {
	path       string
	fileHandle *os.File
	queue      []string
	draining   bool
	closed     bool
	mu         sync.Mutex
	cond       *sync.Cond
	totalBytes int64
}

// NewDiskTaskOutput creates a new disk task output writer
func NewDiskTaskOutput(path string) *DiskTaskOutput {
	dto := &DiskTaskOutput{
		path:  path,
		queue: make([]string, 0),
	}
	dto.cond = sync.NewCond(&dto.mu)
	return dto
}

// Write queues a chunk for writing
func (d *DiskTaskOutput) Write(chunk string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return fmt.Errorf("output writer is closed")
	}

	// Check size limit
	chunkSize := int64(len(chunk))
	if d.totalBytes+chunkSize > MaxTaskOutputBytes {
		return fmt.Errorf("task output exceeds %s limit", MaxTaskOutputBytesDisplay)
	}

	d.queue = append(d.queue, chunk)
	d.totalBytes += chunkSize
	d.cond.Signal()

	// Start drain loop if not already running
	if !d.draining {
		d.draining = true
		go d.drain()
	}

	return nil
}

// drain processes the write queue
func (d *DiskTaskOutput) drain() {
	for {
		d.mu.Lock()

		// Wait for work or close signal
		for len(d.queue) == 0 && !d.closed {
			d.cond.Wait()
		}

		if len(d.queue) == 0 && d.closed {
			d.draining = false
			d.mu.Unlock()
			return
		}

		// Get next chunk
		chunk := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()

		// Open file if needed
		if d.fileHandle == nil {
			f, err := os.OpenFile(d.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				// TODO: Handle error properly
				continue
			}
			d.fileHandle = f
		}

		// Write chunk
		if _, err := d.fileHandle.WriteString(chunk); err != nil {
			// TODO: Handle error properly
			continue
		}
	}
}

// Close closes the output writer
func (d *DiskTaskOutput) Close() error {
	d.mu.Lock()
	d.closed = true
	d.cond.Signal()
	d.mu.Unlock()

	// Wait for drain to finish
	d.mu.Lock()
	for d.draining {
		d.mu.Unlock()
		d.cond.Wait()
		d.mu.Lock()
	}
	d.mu.Unlock()

	if d.fileHandle != nil {
		return d.fileHandle.Close()
	}
	return nil
}

// ReadTaskOutput reads task output from a file
func ReadTaskOutput(path string, offset int, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxReadBytes
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	// Seek to offset
	if offset > 0 {
		if _, err := f.Seek(int64(offset), 0); err != nil {
			return "", err
		}
	}

	// Read up to maxBytes
	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return "", err
	}

	return string(buf[:n]), nil
}

// GetTaskOutputDelta reads new output since the last offset
func GetTaskOutputDelta(path string, lastOffset int) (string, int, error) {
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", lastOffset, nil
		}
		return "", lastOffset, err
	}

	currentSize := int(stat.Size())
	if currentSize <= lastOffset {
		return "", lastOffset, nil
	}

	delta, err := ReadTaskOutput(path, lastOffset, currentSize-lastOffset)
	if err != nil {
		return "", lastOffset, err
	}

	return delta, currentSize, nil
}

// EvictTaskOutput removes a task's output file
func EvictTaskOutput(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// InitTaskOutputAsSymlink creates a symlink for task output
func InitTaskOutputAsSymlink(taskOutputPath, targetPath string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(taskOutputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Remove existing file/symlink
	_ = os.Remove(taskOutputPath)

	// Create symlink
	return os.Symlink(targetPath, taskOutputPath)
}

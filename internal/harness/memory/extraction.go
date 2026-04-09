package memory

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ExtractionService handles background memory extraction.
type ExtractionService struct {
	queue      chan *ExtractionTask
	workers    int
	wg         sync.WaitGroup
	mu         sync.RWMutex
	tasks      map[string]*ExtractionTask
	running    bool
	cancelFunc context.CancelFunc
}

// ExtractionTask represents a memory extraction task.
type ExtractionTask struct {
	ID          string
	SessionID   string
	Content     string
	Type        ExtractionType
	Priority    int
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	Result      *BackgroundExtractionResult
	Error       error
	Status      TaskStatus
}

// ExtractionType defines the type of memory extraction.
type ExtractionType string

const (
	ExtractionTypeUser      ExtractionType = "user"
	ExtractionTypeFeedback  ExtractionType = "feedback"
	ExtractionTypeProject   ExtractionType = "project"
	ExtractionTypeReference ExtractionType = "reference"
)

// TaskStatus represents the status of an extraction task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// BackgroundExtractionResult contains the extracted memory information.
type BackgroundExtractionResult struct {
	Type        ExtractionType
	Name        string
	Description string
	Content     string
	Confidence  float64
	Metadata    map[string]any
}

// NewExtractionService creates a new extraction service.
func NewExtractionService(workers int) *ExtractionService {
	if workers <= 0 {
		workers = 2
	}

	return &ExtractionService{
		queue:   make(chan *ExtractionTask, 100),
		workers: workers,
		tasks:   make(map[string]*ExtractionTask),
		running: false,
	}
}

// Start starts the extraction service.
func (s *ExtractionService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("service already running")
	}
	s.running = true
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	// Start worker goroutines
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(ctx, i)
	}

	return nil
}

// Stop stops the extraction service.
func (s *ExtractionService) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return fmt.Errorf("service not running")
	}
	s.running = false
	s.mu.Unlock()

	// Cancel context and close queue
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
	close(s.queue)

	// Wait for workers to finish
	s.wg.Wait()

	return nil
}

// Submit submits a new extraction task.
func (s *ExtractionService) Submit(task *ExtractionTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("service not running")
	}

	if task.ID == "" {
		task.ID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	task.Status = TaskStatusPending
	task.CreatedAt = time.Now()

	s.tasks[task.ID] = task

	// Send to queue (non-blocking)
	select {
	case s.queue <- task:
		return nil
	default:
		return fmt.Errorf("queue full")
	}
}

// GetTask returns a task by ID.
func (s *ExtractionService) GetTask(id string) (*ExtractionTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	return task, nil
}

// ListTasks returns all tasks.
func (s *ExtractionService) ListTasks() []*ExtractionTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*ExtractionTask, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}

	return tasks
}

// worker processes extraction tasks.
func (s *ExtractionService) worker(ctx context.Context, id int) {
	defer s.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-s.queue:
			if !ok {
				return
			}
			s.processTask(ctx, task)
		}
	}
}

// processTask processes a single extraction task.
func (s *ExtractionService) processTask(ctx context.Context, task *ExtractionTask) {
	now := time.Now()
	task.StartedAt = &now
	task.Status = TaskStatusRunning

	// Extract memory
	result, err := s.extract(ctx, task)

	completedAt := time.Now()
	task.CompletedAt = &completedAt
	task.Result = result
	task.Error = err

	if err != nil {
		task.Status = TaskStatusFailed
	} else {
		task.Status = TaskStatusCompleted
	}
}

// extract performs the actual memory extraction.
func (s *ExtractionService) extract(ctx context.Context, task *ExtractionTask) (*BackgroundExtractionResult, error) {
	// Simulate extraction logic
	// In a real implementation, this would use LLM or pattern matching

	result := &BackgroundExtractionResult{
		Type:        task.Type,
		Name:        fmt.Sprintf("memory-%s", task.ID),
		Description: "Extracted memory",
		Content:     task.Content,
		Confidence:  0.8,
		Metadata:    make(map[string]any),
	}

	return result, nil
}

// Cleanup removes completed tasks older than the specified duration.
func (s *ExtractionService) Cleanup(maxAge time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for id, task := range s.tasks {
		if task.Status == TaskStatusCompleted || task.Status == TaskStatusFailed {
			if task.CompletedAt != nil && now.Sub(*task.CompletedAt) > maxAge {
				delete(s.tasks, id)
				removed++
			}
		}
	}

	return removed
}

// Stats returns service statistics.
func (s *ExtractionService) Stats() *ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &ServiceStats{
		TotalTasks:     len(s.tasks),
		PendingTasks:   0,
		RunningTasks:   0,
		CompletedTasks: 0,
		FailedTasks:    0,
	}

	for _, task := range s.tasks {
		switch task.Status {
		case TaskStatusPending:
			stats.PendingTasks++
		case TaskStatusRunning:
			stats.RunningTasks++
		case TaskStatusCompleted:
			stats.CompletedTasks++
		case TaskStatusFailed:
			stats.FailedTasks++
		}
	}

	return stats
}

// ServiceStats contains service statistics.
type ServiceStats struct {
	TotalTasks     int
	PendingTasks   int
	RunningTasks   int
	CompletedTasks int
	FailedTasks    int
}

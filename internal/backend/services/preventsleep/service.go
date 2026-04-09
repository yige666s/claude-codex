package preventsleep

import (
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const (
	defaultCaffeinateTimeout = 5 * time.Minute
	defaultRestartInterval   = 4 * time.Minute
)

// Process represents the minimal process control surface used by the service.
type Process interface {
	Kill() error
	Done() <-chan struct{}
}

// Timer is the minimal timer surface used by the service.
type Timer interface {
	Stop() bool
}

// Options controls service behavior and test seams.
type Options struct {
	Platform        string
	Timeout         time.Duration
	RestartInterval time.Duration
	Spawn           func(timeout time.Duration) (Process, error)
	Schedule        func(delay time.Duration, fn func()) Timer
	RegisterCleanup func(func())
	Logger          func(string)
}

// Service prevents macOS idle sleep by maintaining a short-lived caffeinate process.
type Service struct {
	mu sync.Mutex

	platform        string
	timeout         time.Duration
	restartInterval time.Duration
	spawn           func(timeout time.Duration) (Process, error)
	schedule        func(delay time.Duration, fn func()) Timer
	registerCleanup func(func())
	logger          func(string)

	refCount          int
	cleanupRegistered bool
	process           Process
	restartTimer      Timer
}

// NewService creates a prevent-sleep service with production defaults.
func NewService(opts *Options) *Service {
	cfg := Options{}
	if opts != nil {
		cfg = *opts
	}
	if cfg.Platform == "" {
		cfg.Platform = runtime.GOOS
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultCaffeinateTimeout
	}
	if cfg.RestartInterval <= 0 {
		cfg.RestartInterval = defaultRestartInterval
	}
	if cfg.Spawn == nil {
		cfg.Spawn = spawnCaffeinateProcess
	}
	if cfg.Schedule == nil {
		cfg.Schedule = func(delay time.Duration, fn func()) Timer {
			return time.AfterFunc(delay, fn)
		}
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}

	return &Service{
		platform:        cfg.Platform,
		timeout:         cfg.Timeout,
		restartInterval: cfg.RestartInterval,
		spawn:           cfg.Spawn,
		schedule:        cfg.Schedule,
		registerCleanup: cfg.RegisterCleanup,
		logger:          cfg.Logger,
	}
}

// Start increments the reference count and enables sleep prevention when needed.
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refCount++
	if s.refCount != 1 {
		return
	}

	s.spawnLocked()
	s.startRestartTimerLocked()
}

// Stop decrements the reference count and disables sleep prevention when idle.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refCount > 0 {
		s.refCount--
	}
	if s.refCount != 0 {
		return
	}

	s.stopRestartTimerLocked()
	s.killProcessLocked("Stopped caffeinate, allowing sleep")
}

// ForceStop clears all state and terminates the tracked caffeinate process.
func (s *Service) ForceStop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refCount = 0
	s.stopRestartTimerLocked()
	s.killProcessLocked("Stopped caffeinate, allowing sleep")
}

// RefCount returns the current reference count.
func (s *Service) RefCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refCount
}

func (s *Service) spawnLocked() {
	if s.platform != "darwin" || s.process != nil {
		return
	}
	if !s.cleanupRegistered && s.registerCleanup != nil {
		s.cleanupRegistered = true
		s.registerCleanup(s.ForceStop)
	}
	proc, err := s.spawn(s.timeout)
	if err != nil {
		s.logger("caffeinate spawn error: " + err.Error())
		return
	}
	if proc == nil {
		return
	}
	s.process = proc
	go s.watchProcess(proc)
	s.logger("Started caffeinate to prevent sleep")
}

func (s *Service) watchProcess(proc Process) {
	<-proc.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.process == proc {
		s.process = nil
	}
}

func (s *Service) startRestartTimerLocked() {
	if s.platform != "darwin" || s.restartTimer != nil {
		return
	}
	s.restartTimer = s.schedule(s.restartInterval, s.handleRestartTimer)
}

func (s *Service) stopRestartTimerLocked() {
	if s.restartTimer == nil {
		return
	}
	s.restartTimer.Stop()
	s.restartTimer = nil
}

func (s *Service) handleRestartTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.restartTimer = nil
	if s.refCount <= 0 {
		return
	}
	s.logger("Restarting caffeinate to maintain sleep prevention")
	s.killProcessLocked("")
	s.spawnLocked()
	s.startRestartTimerLocked()
}

func (s *Service) killProcessLocked(logMessage string) {
	if s.process == nil {
		return
	}
	proc := s.process
	s.process = nil
	_ = proc.Kill()
	if logMessage != "" {
		s.logger(logMessage)
	}
}

type execProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func spawnCaffeinateProcess(timeout time.Duration) (Process, error) {
	seconds := int(timeout / time.Second)
	cmd := exec.Command("caffeinate", "-i", "-t", strconv.Itoa(seconds))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	proc := &execProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(proc.done)
	}()
	return proc, nil
}

func (p *execProcess) Kill() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *execProcess) Done() <-chan struct{} {
	if p == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return p.done
}

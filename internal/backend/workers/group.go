package workers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateFailed  State = "failed"
)

type Status struct {
	Name      string
	State     State
	Error     string
	StartedAt time.Time
	StoppedAt time.Time
}

type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	group  *errgroup.Group
	logger *slog.Logger

	mu       sync.RWMutex
	statuses map[string]Status
	stops    []stopFunc
	doneOnce sync.Once
	done     chan error
}

type stopFunc struct {
	name string
	fn   func(context.Context) error
}

type Option func(*startOptions)

type startOptions struct {
	stop func(context.Context) error
}

func WithStop(stop func(context.Context) error) Option {
	return func(options *startOptions) {
		options.stop = stop
	}
}

func New(parent context.Context, logger *slog.Logger) *Group {
	if parent == nil {
		parent = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	group, ctx := errgroup.WithContext(parent)
	ctx, cancel := context.WithCancel(ctx)
	return &Group{
		ctx:      ctx,
		cancel:   cancel,
		group:    group,
		logger:   logger.With(slog.String("component", "worker_group")),
		statuses: make(map[string]Status),
		done:     make(chan error, 1),
	}
}

func (g *Group) Start(name string, run func(context.Context) error, options ...Option) bool {
	if g == nil || run == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	var start startOptions
	for _, option := range options {
		if option != nil {
			option(&start)
		}
	}
	g.setStatus(Status{Name: name, State: StateRunning, StartedAt: time.Now().UTC()})
	if start.stop != nil {
		g.mu.Lock()
		g.stops = append(g.stops, stopFunc{name: name, fn: start.stop})
		g.mu.Unlock()
	}
	g.group.Go(func() error {
		err := run(g.ctx)
		if err != nil && errors.Is(err, context.Canceled) {
			err = nil
		}
		if err != nil {
			g.setStatus(Status{Name: name, State: StateFailed, Error: err.Error(), StoppedAt: time.Now().UTC()})
			g.logger.ErrorContext(g.ctx, "worker stopped with error", slog.String("worker", name), slog.Any("error", err))
			return err
		}
		g.setStatus(Status{Name: name, State: StateStopped, StoppedAt: time.Now().UTC()})
		g.logger.InfoContext(context.Background(), "worker stopped", slog.String("worker", name))
		return nil
	})
	return true
}

func (g *Group) Stop(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	g.cancel()
	stopErr := g.runStops(ctx)
	waitDone := g.Done()
	select {
	case err := <-waitDone:
		return errors.Join(stopErr, err)
	case <-ctx.Done():
		return errors.Join(stopErr, ctx.Err())
	}
}

func (g *Group) Done() <-chan error {
	if g == nil {
		done := make(chan error)
		close(done)
		return done
	}
	g.doneOnce.Do(func() {
		go func() {
			g.done <- g.group.Wait()
			close(g.done)
		}()
	})
	return g.done
}

func (g *Group) Context() context.Context {
	if g == nil || g.ctx == nil {
		return context.Background()
	}
	return g.ctx
}

func (g *Group) Started(name string) bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	status, ok := g.statuses[name]
	return ok && status.State == StateRunning
}

func (g *Group) ReadinessCheck() func(context.Context) error {
	return func(context.Context) error {
		if g == nil {
			return nil
		}
		for _, status := range g.Snapshot() {
			if status.State == StateFailed {
				return fmt.Errorf("worker %s failed: %s", status.Name, status.Error)
			}
		}
		return nil
	}
}

func (g *Group) Snapshot() []Status {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Status, 0, len(g.statuses))
	for _, status := range g.statuses {
		out = append(out, status)
	}
	return out
}

func (g *Group) setStatus(status Status) {
	g.mu.Lock()
	defer g.mu.Unlock()
	previous := g.statuses[status.Name]
	if status.StartedAt.IsZero() {
		status.StartedAt = previous.StartedAt
	}
	g.statuses[status.Name] = status
}

func (g *Group) runStops(ctx context.Context) error {
	g.mu.RLock()
	stops := append([]stopFunc(nil), g.stops...)
	g.mu.RUnlock()
	var err error
	for i := len(stops) - 1; i >= 0; i-- {
		stop := stops[i]
		if stop.fn == nil {
			continue
		}
		if stopErr := stop.fn(ctx); stopErr != nil && !errors.Is(stopErr, context.Canceled) {
			g.logger.WarnContext(ctx, "worker stop failed", slog.String("worker", stop.name), slog.Any("error", stopErr))
			err = errors.Join(err, stopErr)
		}
	}
	return err
}

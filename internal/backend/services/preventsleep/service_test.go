package preventsleep

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeProcess struct {
	kills int
	done  chan struct{}
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan struct{})}
}

func (p *fakeProcess) Kill() error {
	p.kills++
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	return nil
}

func (p *fakeProcess) Done() <-chan struct{} { return p.done }

type fakeTimer struct {
	stops int
	fn    func()
}

func (t *fakeTimer) Stop() bool {
	t.stops++
	return true
}

func TestStartStopReferenceCounting(t *testing.T) {
	spawnCalls := 0
	proc := newFakeProcess()
	var timers []*fakeTimer
	service := NewService(&Options{
		Platform: "darwin",
		Spawn: func(timeout time.Duration) (Process, error) {
			spawnCalls++
			return proc, nil
		},
		Schedule: func(delay time.Duration, fn func()) Timer {
			timer := &fakeTimer{fn: fn}
			timers = append(timers, timer)
			return timer
		},
	})

	service.Start()
	service.Start()
	if got := service.RefCount(); got != 2 {
		t.Fatalf("RefCount = %d, want 2", got)
	}
	if spawnCalls != 1 {
		t.Fatalf("spawnCalls = %d, want 1", spawnCalls)
	}
	if len(timers) != 1 {
		t.Fatalf("timers = %d, want 1", len(timers))
	}

	service.Stop()
	if proc.kills != 0 {
		t.Fatalf("kills after partial stop = %d, want 0", proc.kills)
	}

	service.Stop()
	if proc.kills != 1 {
		t.Fatalf("kills after final stop = %d, want 1", proc.kills)
	}
	if timers[0].stops != 1 {
		t.Fatalf("timer stops = %d, want 1", timers[0].stops)
	}
}

func TestNonDarwinDoesNotSpawn(t *testing.T) {
	spawnCalls := 0
	service := NewService(&Options{
		Platform: "linux",
		Spawn: func(timeout time.Duration) (Process, error) {
			spawnCalls++
			return newFakeProcess(), nil
		},
	})

	service.Start()
	service.Stop()

	if spawnCalls != 0 {
		t.Fatalf("spawnCalls = %d, want 0", spawnCalls)
	}
}

func TestRestartTimerRespawnsProcess(t *testing.T) {
	var spawned []*fakeProcess
	var timers []*fakeTimer
	service := NewService(&Options{
		Platform: "darwin",
		Spawn: func(timeout time.Duration) (Process, error) {
			proc := newFakeProcess()
			spawned = append(spawned, proc)
			return proc, nil
		},
		Schedule: func(delay time.Duration, fn func()) Timer {
			timer := &fakeTimer{fn: fn}
			timers = append(timers, timer)
			return timer
		},
	})

	service.Start()
	if len(timers) != 1 {
		t.Fatalf("timers = %d, want 1", len(timers))
	}

	timers[0].fn()

	if len(spawned) != 2 {
		t.Fatalf("spawned = %d, want 2", len(spawned))
	}
	if spawned[0].kills != 1 {
		t.Fatalf("first process kills = %d, want 1", spawned[0].kills)
	}
	if len(timers) != 2 {
		t.Fatalf("timers after restart = %d, want 2", len(timers))
	}
}

func TestRegisterCleanupOnlyOnce(t *testing.T) {
	registered := 0
	var cleanup func()
	service := NewService(&Options{
		Platform: "darwin",
		Spawn: func(timeout time.Duration) (Process, error) {
			return newFakeProcess(), nil
		},
		Schedule: func(delay time.Duration, fn func()) Timer { return &fakeTimer{fn: fn} },
		RegisterCleanup: func(fn func()) {
			registered++
			cleanup = fn
		},
	})

	service.Start()
	service.ForceStop()
	service.Start()

	if registered != 1 {
		t.Fatalf("registered = %d, want 1", registered)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup callback")
	}
}

func TestSpawnErrorLoggedAndRetriedByTimer(t *testing.T) {
	logs := []string{}
	attempt := 0
	var timer *fakeTimer
	service := NewService(&Options{
		Platform: "darwin",
		Spawn: func(timeout time.Duration) (Process, error) {
			attempt++
			if attempt == 1 {
				return nil, errors.New("boom")
			}
			return newFakeProcess(), nil
		},
		Schedule: func(delay time.Duration, fn func()) Timer {
			timer = &fakeTimer{fn: fn}
			return timer
		},
		Logger: func(msg string) { logs = append(logs, msg) },
	})

	service.Start()
	if attempt != 1 {
		t.Fatalf("attempt = %d, want 1", attempt)
	}
	if timer == nil {
		t.Fatal("expected timer to be scheduled")
	}

	timer.fn()
	if attempt != 2 {
		t.Fatalf("attempt after retry = %d, want 2", attempt)
	}
	want := []string{"caffeinate spawn error: boom", "Restarting caffeinate to maintain sleep prevention", "Started caffeinate to prevent sleep"}
	if !reflect.DeepEqual(logs, want) {
		t.Fatalf("logs = %#v, want %#v", logs, want)
	}
}

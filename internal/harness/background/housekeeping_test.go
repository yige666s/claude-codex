package background

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerRunsDelayedAndRecurringTasks(t *testing.T) {
	var delayed atomic.Int64
	var recurring atomic.Int64

	scheduler := NewScheduler(Options{
		StartupDelay:      5 * time.Millisecond,
		RecurringInterval: 10 * time.Millisecond,
		Now:               time.Now,
		AfterFunc: func(d time.Duration, fn func()) Timer {
			timer := time.AfterFunc(d, fn)
			return timer
		},
	})

	stop := scheduler.Start(Tasks{
		RunDelayed:   func() { delayed.Add(1) },
		RunRecurring: func() { recurring.Add(1) },
	})
	defer stop()

	time.Sleep(35 * time.Millisecond)
	if delayed.Load() == 0 {
		t.Fatal("expected delayed task to run")
	}
	if recurring.Load() == 0 {
		t.Fatal("expected recurring task to run")
	}
}

func TestSchedulerDefersDelayedTaskAfterRecentInteraction(t *testing.T) {
	var delayed atomic.Int64
	lastInteraction := time.Now()
	scheduler := NewScheduler(Options{
		StartupDelay:       5 * time.Millisecond,
		RecurringInterval:  0,
		RecentWindow:       time.Hour,
		Now:                time.Now,
		GetLastInteraction: func() time.Time { return lastInteraction },
		AfterFunc: func(d time.Duration, fn func()) Timer {
			timer := time.AfterFunc(d, fn)
			return timer
		},
	})

	stop := scheduler.Start(Tasks{
		RunDelayed: func() { delayed.Add(1) },
	})
	defer stop()

	time.Sleep(20 * time.Millisecond)
	if delayed.Load() != 0 {
		t.Fatalf("expected delayed task to stay deferred, got %d", delayed.Load())
	}
}

package workers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGroupStopCancelsWorkersAndRunsStops(t *testing.T) {
	group := New(context.Background(), nil)
	stopped := make(chan struct{})
	stopCalled := false

	group.Start("test", func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}, WithStop(func(context.Context) error {
		stopCalled = true
		return nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("worker was not cancelled")
	}
	if !stopCalled {
		t.Fatal("stop hook was not called")
	}
	if err := group.ReadinessCheck()(context.Background()); err != nil {
		t.Fatalf("ReadinessCheck() error = %v", err)
	}
}

func TestGroupReadinessReportsWorkerFailure(t *testing.T) {
	group := New(context.Background(), nil)
	group.Start("failing", func(context.Context) error {
		return errors.New("boom")
	})

	deadline := time.After(time.Second)
	for {
		err := group.ReadinessCheck()(context.Background())
		if err != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("readiness did not report worker failure")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestGroupDoneReportsCompletion(t *testing.T) {
	group := New(context.Background(), nil)
	group.Start("done", func(context.Context) error {
		return nil
	})
	select {
	case err := <-group.Done():
		if err != nil {
			t.Fatalf("Done() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Done() did not complete")
	}
}

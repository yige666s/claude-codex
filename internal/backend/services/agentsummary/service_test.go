package agentsummary

import (
	"testing"
	"time"
)

func TestAgentSummaryScheduler(t *testing.T) {
	service := NewService(10 * time.Millisecond)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	done := make(chan string, 1)
	stop := service.Start(func(previous string) (string, error) {
		if previous == "" {
			return "Reading runAgent.ts", nil
		}
		return previous, nil
	}, func(summary string) {
		done <- summary
	})
	defer stop()
	select {
	case summary := <-done:
		if summary != "Reading runAgent.ts" {
			t.Fatalf("unexpected summary: %s", summary)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for summary")
	}
}

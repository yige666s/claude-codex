package tips

import "testing"

func TestTipHistoryAndScheduler(t *testing.T) {
	history := &History{NumStartups: 30, TipsHistory: map[string]int{"memory-command": 2, "theme-command": 29, "status-line": 28}}
	registry := NewRegistry()
	tip := GetTipToShow(history, registry, nil, true)
	if tip == nil || tip.ID != "memory-command" {
		t.Fatalf("unexpected tip: %+v", tip)
	}
	history.RecordTipShown(tip.ID)
	if history.SessionsSinceLastShown(tip.ID) != 0 {
		t.Fatalf("expected shown tip to reset history")
	}
}

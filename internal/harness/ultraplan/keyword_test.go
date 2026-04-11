package ultraplan

import "testing"

func TestFindUltraplanTriggerPositionsSkipsQuotedAndPathLikeUses(t *testing.T) {
	if got := FindUltraplanTriggerPositions(`please ultraplan this`); len(got) != 1 {
		t.Fatalf("expected one trigger, got %#v", got)
	}
	if got := FindUltraplanTriggerPositions(`"ultraplan"`); len(got) != 0 {
		t.Fatalf("expected quoted text to be ignored, got %#v", got)
	}
	if got := FindUltraplanTriggerPositions(`src/ultraplan/file.ts`); len(got) != 0 {
		t.Fatalf("expected path-like text to be ignored, got %#v", got)
	}
	if got := FindUltraplanTriggerPositions(`/rename ultraplan foo`); len(got) != 0 {
		t.Fatalf("expected slash command text to be ignored, got %#v", got)
	}
}

func TestReplaceUltraplanKeyword(t *testing.T) {
	got := ReplaceUltraplanKeyword("please ultraplan this")
	if got != "please plan this" {
		t.Fatalf("unexpected replacement %q", got)
	}
}

package utils

import "testing"

func TestStripBOM(t *testing.T) {
	if got := StripBOM("\uFEFFhello"); got != "hello" {
		t.Fatalf("unexpected strip result %q", got)
	}
	if got := StripBOM("hello"); got != "hello" {
		t.Fatalf("unexpected unchanged result %q", got)
	}
}

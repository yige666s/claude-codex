package utils

import "testing"

func TestHashHelpers(t *testing.T) {
	if DJB2Hash("abc") == 0 {
		t.Fatal("expected non-zero djb2 hash")
	}
	if HashContent("abc") == HashContent("def") {
		t.Fatal("expected different content hashes")
	}
	if HashPair("a", "bc") == HashPair("ab", "c") {
		t.Fatal("expected pair hash to disambiguate input boundaries")
	}
}

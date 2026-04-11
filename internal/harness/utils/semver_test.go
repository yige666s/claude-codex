package utils

import "testing"

func TestSemverHelpers(t *testing.T) {
	if !GT("1.2.3", "1.2.2") || !GTE("1.2.3", "1.2.3") {
		t.Fatal("expected greater-than helpers to work")
	}
	if !LT("1.2.2", "1.2.3") || !LTE("1.2.3", "1.2.3") {
		t.Fatal("expected less-than helpers to work")
	}
	if Order("1.2.3", "1.2.4") != -1 {
		t.Fatal("expected order=-1")
	}
	if !Satisfies("1.2.3", ">=1.2.0 <2.0.0") {
		t.Fatal("expected range satisfaction")
	}
	if !Satisfies("1.2.3", "^1.0.0") {
		t.Fatal("expected caret range satisfaction")
	}
}

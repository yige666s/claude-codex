package utils

import "testing"

func TestArrayHelpers(t *testing.T) {
	got := Intersperse([]int{1, 2, 3}, func(_ int) int { return 0 })
	if len(got) != 5 || got[1] != 0 || got[3] != 0 {
		t.Fatalf("unexpected intersperse result %#v", got)
	}
	if Count([]int{1, 2, 3, 4}, func(v int) bool { return v%2 == 0 }) != 2 {
		t.Fatal("expected even count 2")
	}
	uniq := Uniq([]string{"a", "b", "a", "c"})
	if len(uniq) != 3 {
		t.Fatalf("unexpected uniq %#v", uniq)
	}
}

package buddy

import "testing"

func TestRollDeterministic(t *testing.T) {
	a := Roll("user-1")
	b := Roll("user-1")
	if a.Bones.Species != b.Bones.Species || a.Bones.Rarity != b.Bones.Rarity {
		t.Fatalf("expected deterministic roll, got %#v vs %#v", a, b)
	}
}

func TestCompanionIntroText(t *testing.T) {
	text := CompanionIntroText("Mochi", "duck")
	if text == "" || text == "# Companion" {
		t.Fatalf("unexpected intro text %q", text)
	}
}

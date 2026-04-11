package vcr

import (
	"context"
	"testing"
)

func TestWithFixture(t *testing.T) {
	service := NewService(t.TempDir(), true, false)
	callCount := 0
	input := map[string]any{"x": 1}
	first, err := service.WithFixture(context.Background(), input, "test", func(context.Context) (any, error) {
		callCount++
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatalf("WithFixture first: %v", err)
	}
	second, err := service.WithFixture(context.Background(), input, "test", func(context.Context) (any, error) {
		callCount++
		return map[string]any{"ok": false}, nil
	})
	if err != nil {
		t.Fatalf("WithFixture second: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected fixture reuse, got %d calls", callCount)
	}
	if first.(map[string]any)["ok"] != true || second.(map[string]any)["ok"] != true {
		t.Fatalf("unexpected fixture values: %#v %#v", first, second)
	}
}

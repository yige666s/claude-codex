package sandboxartifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/backend/sandboxtransfer"
)

func fixture(t *testing.T) (Store, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "skills")
	skill := filepath.Join(root, "demo")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: demo\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "run.py"), []byte("print('v1')"), 0600); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(base, "artifacts"), Builder: sandboxtransfer.Builder{SkillRoots: []string{root}, Limits: sandboxtransfer.DevLimits()}}
	return store, skill
}
func TestPublishImmutableVersionsAndCache(t *testing.T) {
	store, skill := fixture(t)
	v1, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Get(v1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Publish(context.Background(), "demo", skill)
	if err != nil || again != v1 {
		t.Fatalf("unstable digest: %+v %v", again, err)
	}
	if err := os.WriteFile(filepath.Join(skill, "run.py"), []byte("print('v2')"), 0600); err != nil {
		t.Fatal(err)
	}
	v2, err := store.Publish(context.Background(), "demo", skill)
	if err != nil || v1 == v2 {
		t.Fatalf("version unchanged: %+v %v", v2, err)
	}
	active, err := store.Resolve("demo")
	if err != nil || active != v2 {
		t.Fatalf("active ref: %+v %v", active, err)
	}
	old, err := store.Get(v1)
	if err != nil || string(old) != string(first) {
		t.Fatalf("old version lost: %v", err)
	}
	calls := 0
	cache := Cache{Dir: filepath.Join(t.TempDir(), "cache"), Limits: sandboxtransfer.DevLimits(), Source: FetchFunc(func(ctx context.Context, ref Ref) ([]byte, error) { calls++; return store.Get(ref) })}
	p1, err := cache.Ensure(context.Background(), v1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := cache.Ensure(context.Background(), v1)
	if err != nil || p1 != p2 || calls != 1 {
		t.Fatalf("cache miss twice: %s %s calls=%d %v", p1, p2, calls, err)
	}
	content, err := os.ReadFile(filepath.Join(p1, "run.py"))
	if err != nil || string(content) != "print('v1')" {
		t.Fatalf("unexpected cached content: %s %v", content, err)
	}
	if _, err := cache.Ensure(context.Background(), v2); err != nil || calls != 2 {
		t.Fatalf("new version not fetched: %d %v", calls, err)
	}
}
func TestCacheFailsClosed(t *testing.T) {
	store, skill := fixture(t)
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	cache := Cache{Dir: filepath.Join(t.TempDir(), "cache"), Limits: sandboxtransfer.DevLimits(), Source: FetchFunc(func(context.Context, Ref) ([]byte, error) { return []byte("bad"), nil })}
	if _, err := cache.Ensure(context.Background(), ref); err == nil {
		t.Fatal("accepted wrong digest")
	}
	cache.Source = FetchFunc(func(ctx context.Context, ref Ref) ([]byte, error) { return store.Get(ref) })
	path, err := cache.Ensure(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(path, "run.py"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "run.py"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(context.Background(), ref); err == nil {
		t.Fatal("accepted tampered cache")
	}
	if _, err := cache.Ensure(context.Background(), Ref{ID: "../bad", Digest: ref.Digest}); err == nil {
		t.Fatal("accepted invalid name")
	}
	if _, err := cache.Ensure(context.Background(), Ref{ID: "demo", Digest: "sha256:" + strings.Repeat("X", 64)}); err == nil {
		t.Fatal("accepted invalid digest")
	}
	cache.Source = FetchFunc(func(context.Context, Ref) ([]byte, error) { return nil, errors.New("offline") })
	other := Ref{ID: "demo", Digest: "sha256:" + strings.Repeat("a", 64)}
	if _, err := cache.Ensure(context.Background(), other); err == nil {
		t.Fatal("silent fallback on cache miss")
	}
}

func TestPublishedIDCannotBorrowAnotherArtifact(t *testing.T) {
	store, skill := fixture(t)
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	unbound := Ref{ID: "other", Digest: ref.Digest}
	if _, err := store.Get(unbound); err == nil {
		t.Fatal("unpublished id could borrow digest")
	}
	if _, err := store.Resolve("../demo"); err == nil {
		t.Fatal("accepted path traversal")
	}
}
func TestCacheCapacityFailsClosed(t *testing.T) {
	store, skill := fixture(t)
	a, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "run.py"), []byte("print('new')"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	cache := Cache{Dir: filepath.Join(t.TempDir(), "cache"), Limits: sandboxtransfer.DevLimits(), MaxEntries: 1, Source: FetchFunc(func(ctx context.Context, ref Ref) ([]byte, error) { return store.Get(ref) })}
	if _, err := cache.Ensure(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Ensure(context.Background(), b); err == nil {
		t.Fatal("capacity exceeded")
	}
	if _, err := cache.Ensure(context.Background(), a); err != nil {
		t.Fatalf("cached version lost: %v", err)
	}
}

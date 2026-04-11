package securestorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"claude-codex/internal/app/config"
)

func TestPlaintextStoreRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".credentials.json")
	store := NewPlaintextStore(path)

	input := Data{
		"claudeAiOauth": map[string]any{
			"accessToken":  "access-token",
			"refreshToken": "refresh-token",
		},
	}

	result, err := store.Write(input)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if result.Warning != plaintextWarning {
		t.Fatalf("Write() warning = %q, want %q", result.Warning, plaintextWarning)
	}

	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("Read() = %#v, want %#v", got, input)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestFallbackStoreReadsSecondaryWhenPrimaryUnavailable(t *testing.T) {
	t.Parallel()

	secondaryData := Data{"token": "secondary"}
	store := NewFallbackStore(
		&fakeStore{name: "primary", readErr: errors.New("boom")},
		&fakeStore{name: "secondary", readData: secondaryData},
	)

	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(got, secondaryData) {
		t.Fatalf("Read() = %#v, want %#v", got, secondaryData)
	}
}

func TestFallbackStoreMigratesToPrimaryAndDeletesSecondary(t *testing.T) {
	t.Parallel()

	primary := &fakeStore{name: "primary"}
	secondary := &fakeStore{name: "secondary"}
	store := NewFallbackStore(primary, secondary)

	input := Data{"token": "fresh"}
	if _, err := store.Write(input); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if !reflect.DeepEqual(primary.writeData, input) {
		t.Fatalf("primary write data = %#v, want %#v", primary.writeData, input)
	}
	if secondary.deleteCalls != 1 {
		t.Fatalf("secondary delete calls = %d, want 1", secondary.deleteCalls)
	}
}

func TestFallbackStoreUsesSecondaryWhenPrimaryWriteFails(t *testing.T) {
	t.Parallel()

	primary := &fakeStore{
		name:     "primary",
		readData: Data{"token": "stale"},
		writeErr: errors.New("write failed"),
	}
	secondary := &fakeStore{name: "secondary"}
	store := NewFallbackStore(primary, secondary)

	input := Data{"token": "fresh"}
	if _, err := store.Write(input); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if !reflect.DeepEqual(secondary.writeData, input) {
		t.Fatalf("secondary write data = %#v, want %#v", secondary.writeData, input)
	}
	if primary.deleteCalls != 1 {
		t.Fatalf("primary delete calls = %d, want 1", primary.deleteCalls)
	}
}

func TestKeychainStoreUsesRunnerForCRUD(t *testing.T) {
	t.Setenv("CLAUDE_GO_HOME", t.TempDir())
	ClearKeychainCache()

	runner := &fakeRunner{
		responses: map[string][]byte{
			"find-generic-password|-a|alice|-w|-s|Claude Codex": []byte(`{"token":"from-keychain"}`),
		},
	}

	store := NewKeychainStore("Claude Codex", "alice")
	store.run = runner.run

	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if want := (Data{"token": "from-keychain"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Read() = %#v, want %#v", got, want)
	}

	if _, err := store.Write(Data{"token": "updated"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	wantCalls := [][]string{
		{"find-generic-password", "-a", "alice", "-w", "-s", "Claude Codex"},
		{"add-generic-password", "-U", "-a", "alice", "-s", "Claude Codex", "-w", `{"token":"updated"}`},
		{"delete-generic-password", "-a", "alice", "-s", "Claude Codex"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestKeychainStoreReadUsesCache(t *testing.T) {
	t.Setenv("CLAUDE_GO_HOME", t.TempDir())

	runner := &fakeRunner{
		responses: map[string][]byte{
			"find-generic-password|-a|alice|-w|-s|Claude Codex": []byte(`{"token":"cached"}`),
		},
	}

	store := NewKeychainStore("Claude Codex", "alice")
	store.run = runner.run
	ClearKeychainCache()

	first, err := store.Read()
	if err != nil {
		t.Fatalf("Read() first error = %v", err)
	}
	second, err := store.Read()
	if err != nil {
		t.Fatalf("Read() second error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached reads mismatch: %#v vs %#v", first, second)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 runner call with cache, got %d", len(runner.calls))
	}
}

func TestStartKeychainPrefetchWarmsCache(t *testing.T) {
	t.Setenv("CLAUDE_GO_HOME", t.TempDir())

	runner := &fakeRunner{
		responses: map[string][]byte{
			"find-generic-password|-a|alice|-w|-s|Claude Codex": []byte(`{"token":"prefetched"}`),
		},
	}
	store := NewKeychainStore("Claude Codex", "alice")
	store.run = runner.run
	ClearKeychainCache()

	StartKeychainPrefetch(store)
	if err := EnsureKeychainPrefetchCompleted(context.Background(), store); err != nil {
		t.Fatalf("EnsureKeychainPrefetchCompleted() error = %v", err)
	}

	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got["token"] != "prefetched" {
		t.Fatalf("prefetched token = %#v", got)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected prefetch to avoid second read call, got %d calls", len(runner.calls))
	}
}

func TestNewStoreFromConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.SecretStore = "plaintext"
	store, err := NewStoreFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewStoreFromConfig(plaintext) error = %v", err)
	}
	if store.Name() != "plaintext" {
		t.Fatalf("store name = %q", store.Name())
	}

	cfg.SecretStore = "auto"
	store, err = NewStoreFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewStoreFromConfig(auto) error = %v", err)
	}
	if store == nil {
		t.Fatal("expected auto store")
	}
}

func TestDataStoreHelpers(t *testing.T) {
	t.Parallel()

	store := &fakeStore{name: "fake", readData: Data{}}
	dataStore := NewDataStore(store)
	if err := dataStore.Set(KeyMCPOAuth, map[string]any{"server": "token"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	var got map[string]any
	ok, err := dataStore.Get(KeyMCPOAuth, &got)
	if err != nil || !ok {
		t.Fatalf("Get() err=%v ok=%v", err, ok)
	}
	if got["server"] != "token" {
		t.Fatalf("unexpected data: %#v", got)
	}
	if err := dataStore.DeleteKey(KeyMCPOAuth); err != nil {
		t.Fatalf("DeleteKey() error = %v", err)
	}
	ok, err = dataStore.Get(KeyMCPOAuth, &got)
	if err != nil {
		t.Fatalf("Get() after delete error = %v", err)
	}
	if ok {
		t.Fatal("expected missing key after delete")
	}
}

type fakeStore struct {
	name        string
	readData    Data
	readErr     error
	writeData   Data
	writeErr    error
	deleteErr   error
	deleteCalls int
}

func (s *fakeStore) Name() string { return s.name }

func (s *fakeStore) Read() (Data, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.readData, nil
}

func (s *fakeStore) Write(data Data) (WriteResult, error) {
	if s.writeErr != nil {
		return WriteResult{}, s.writeErr
	}
	s.writeData = data
	s.readData = data
	return WriteResult{}, nil
}

func (s *fakeStore) Delete() error {
	s.deleteCalls++
	s.readData = nil
	return s.deleteErr
}

type fakeRunner struct {
	calls     [][]string
	responses map[string][]byte
	errs      map[string]error
}

func (r *fakeRunner) run(args ...string) ([]byte, error) {
	copied := append([]string(nil), args...)
	r.calls = append(r.calls, copied)

	key := joinArgs(args)
	if err := r.errs[key]; err != nil {
		return nil, err
	}
	if out := r.responses[key]; out != nil {
		return out, nil
	}
	return nil, nil
}

func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += "|"
		}
		result += arg
	}
	return result
}

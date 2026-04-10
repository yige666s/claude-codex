package securestorage

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
	t.Parallel()

	runner := &fakeRunner{
		responses: map[string][]byte{
			"find-generic-password|-a|alice|-w|-s|Claude Go": []byte(`{"token":"from-keychain"}`),
		},
	}

	store := NewKeychainStore("Claude Go", "alice")
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
		{"find-generic-password", "-a", "alice", "-w", "-s", "Claude Go"},
		{"add-generic-password", "-U", "-a", "alice", "-s", "Claude Go", "-w", `{"token":"updated"}`},
		{"delete-generic-password", "-a", "alice", "-s", "Claude Go"},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, wantCalls)
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

package sandboxnode

import (
	"bytes"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistentNonceSurvivesRestartAndBindsBody(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nonces.log")
	key := bytes.Repeat([]byte("k"), 32)
	store, err := OpenFileNonceStore(file, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewPersistentTaskSigner(key, store)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/dev/execute", bytes.NewReader([]byte("fake")))
	if err := signer.Sign(request, []byte("fake")); err != nil {
		t.Fatal(err)
	}
	if err := signer.Verify(request, []byte("fake")); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileNonceStore(file, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewPersistentTaskSigner(key, reopened)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Verify(request, []byte("fake")); err == nil {
		t.Fatal("nonce replayed across process restart")
	}
	changed, _ := http.NewRequest(http.MethodPost, "http://localhost/v1/dev/execute", nil)
	changed.Header = request.Header.Clone()
	if err := restarted.Verify(changed, []byte("different")); err == nil {
		t.Fatal("signature accepted changed body")
	}
}

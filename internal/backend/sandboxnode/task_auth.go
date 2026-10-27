package sandboxnode

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// TaskSigner provides short-lived, request-bound authentication for simulated
// lifecycle calls. It does not authorize native execution.
type TaskSigner struct {
	key  []byte
	now  func() time.Time
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewTaskSigner(key []byte, now func() time.Time) (*TaskSigner, error) {
	if len(key) < 32 {
		return nil, errors.New("task signing key is too short")
	}
	if now == nil {
		now = time.Now
	}
	return &TaskSigner{key: append([]byte(nil), key...), now: now, seen: map[string]time.Time{}}, nil
}
func (s *TaskSigner) Sign(r *http.Request, body []byte) error {
	if s == nil {
		return errors.New("task signer unavailable")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	ts := strconv.FormatInt(s.now().Unix(), 10)
	id := hex.EncodeToString(nonce[:])
	r.Header.Set("X-Sandbox-Timestamp", ts)
	r.Header.Set("X-Sandbox-Nonce", id)
	r.Header.Set("X-Sandbox-Signature", hex.EncodeToString(s.mac(r.Method, r.URL.RequestURI(), ts, id, body)))
	return nil
}
func (s *TaskSigner) Verify(r *http.Request, body []byte) error {
	if s == nil {
		return errors.New("task signer unavailable")
	}
	ts := r.Header.Get("X-Sandbox-Timestamp")
	id := r.Header.Get("X-Sandbox-Nonce")
	signature := r.Header.Get("X-Sandbox-Signature")
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(id) != 32 || len(signature) != 64 {
		return errors.New("invalid task signature")
	}
	if _, err = hex.DecodeString(id); err != nil {
		return errors.New("invalid task nonce")
	}
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return errors.New("invalid task signature")
	}
	age := s.now().Sub(time.Unix(n, 0))
	if age > 30*time.Second || age < -30*time.Second {
		return errors.New("expired task signature")
	}
	if !hmac.Equal(sig, s.mac(r.Method, r.URL.RequestURI(), ts, id, body)) {
		return errors.New("invalid task signature")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.seen {
		if s.now().Sub(t) > time.Minute {
			delete(s.seen, k)
		}
	}
	if _, exists := s.seen[id]; exists {
		return errors.New("replayed task signature")
	}
	if len(s.seen) >= 4096 {
		return errors.New("task replay cache full")
	}
	s.seen[id] = s.now()
	return nil
}
func (s *TaskSigner) mac(method, path, ts, nonce string, body []byte) []byte {
	hash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, s.key)
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s\n%s\n%x", method, path, ts, nonce, hash)
	return mac.Sum(nil)
}
func limitedBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil {
		return nil, err
	}
	if len(data) > 16384 {
		return nil, errors.New("sandbox request too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return data, nil
}

package sandboxnode

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileNonceStore is only valid while the Node root's exclusive process lock
// is held. It durably records each accepted HMAC nonce before dispatch.
type FileNonceStore struct {
	mu    sync.Mutex
	path  string
	seen  map[string]int64
	count int
}

const maxNonceFileBytes = 4 << 20
const nonceRetention = 2 * time.Minute

func OpenFileNonceStore(path string, now time.Time) (*FileNonceStore, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("nonce path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxNonceFileBytes) {
		return nil, errors.New("nonce file is unsafe")
	}
	if errors.Is(err, os.ErrNotExist) {
		first, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return nil, createErr
		}
		if createErr = first.Sync(); createErr != nil {
			first.Close()
			return nil, createErr
		}
		if createErr = first.Close(); createErr != nil {
			return nil, createErr
		}
		dir, openErr := os.Open(filepath.Dir(path))
		if openErr != nil {
			return nil, openErr
		}
		if openErr = dir.Sync(); openErr != nil {
			dir.Close()
			return nil, openErr
		}
		if openErr = dir.Close(); openErr != nil {
			return nil, openErr
		}
		info, err = os.Lstat(path)
		if err != nil {
			return nil, err
		}
	}
	store := &FileNonceStore{path: path, seen: map[string]int64{}}
	if err == nil {
		file, openErr := os.Open(path)
		if openErr != nil {
			return nil, openErr
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) != 2 || len(fields[0]) != 32 {
				return nil, errors.New("nonce journal corrupt")
			}
			if raw, err := hex.DecodeString(fields[0]); err != nil || hex.EncodeToString(raw) != fields[0] {
				return nil, errors.New("nonce journal corrupt")
			}
			ts, parseErr := strconv.ParseInt(fields[1], 10, 64)
			if parseErr != nil {
				return nil, errors.New("nonce journal corrupt")
			}
			if now.Sub(time.Unix(ts, 0)) < nonceRetention {
				store.seen[fields[0]] = ts
			}
			store.count++
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	// Compact during startup under the external exclusive Node lock.
	if store.count > 0 {
		if err := store.compact(); err != nil {
			return nil, err
		}
	}
	return store, nil
}
func (s *FileNonceStore) Record(id string, now time.Time) error {
	if s == nil || len(id) != 32 {
		return errors.New("invalid nonce store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return errors.New("replayed task signature")
	}
	if s.count >= 8192 {
		for old, ts := range s.seen {
			if now.Sub(time.Unix(ts, 0)) >= nonceRetention {
				delete(s.seen, old)
			}
		}
		if err := s.compact(); err != nil {
			return err
		}
	}
	if len(s.seen) >= 4096 {
		return errors.New("nonce journal capacity reached")
	}
	file, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxNonceFileBytes-64 {
		file.Close()
		return errors.New("nonce journal unsafe or full")
	}
	_, err = fmt.Fprintf(file, "%s %d\n", id, now.Unix())
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("nonce journal persistence failed")
	}
	s.seen[id] = now.Unix()
	s.count++
	return nil
}
func (s *FileNonceStore) compact() error {
	dir := filepath.Dir(s.path)
	temp, err := os.CreateTemp(dir, ".sandbox-nonce-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	for id, ts := range s.seen {
		if _, err := fmt.Fprintf(temp, "%s %d\n", id, ts); err != nil {
			temp.Close()
			return err
		}
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), s.path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	s.count = len(s.seen)
	return nil
}

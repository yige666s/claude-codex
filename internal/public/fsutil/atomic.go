package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	tmpName := tmp.Name()
	cleanup := func(originalErr error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return originalErr
	}

	if _, err := tmp.Write(data); err != nil {
		return cleanup(err)
	}

	if err := tmp.Chmod(perm); err != nil {
		return cleanup(err)
	}

	if err := tmp.Close(); err != nil {
		return cleanup(err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return cleanup(fmt.Errorf("rename %s to %s: %w", tmpName, path, err))
	}

	return nil
}

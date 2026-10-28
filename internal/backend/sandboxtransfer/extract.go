package sandboxtransfer

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ExtractSnapshot expects an empty, private directory created by the trusted
// Node. On any error, its caller must discard the entire directory; partial
// files must never be handed to an executor.
func ExtractSnapshot(data []byte, destination string, lim Limits) error {
	return extractArchive(data, destination, lim, "")
}

// ExtractWorkspace/ExtractSkill reject entries from the other archive class.
func ExtractWorkspace(data []byte, destination string, lim Limits) error {
	return extractArchive(data, destination, lim, "workspace")
}
func ExtractSkill(data []byte, destination string, lim Limits) error {
	return extractArchive(data, destination, lim, "skill")
}

func extractArchive(data []byte, destination string, lim Limits, only string) error {
	if err := lim.validate(); err != nil {
		return err
	}
	if len(data) > int(lim.MaxBytes)+lim.MaxFiles*4096+2048 {
		return errors.New("snapshot archive exceeds limit")
	}
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("snapshot destination is not a real directory")
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("snapshot destination must be empty")
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	reader := tar.NewReader(bytes.NewReader(data))
	seen := make(map[string]bool)
	count := 0
	var total int64
	for {
		hdr, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name, err := validArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("duplicate snapshot entry %q", name)
		}
		if only != "" && name != only && !strings.HasPrefix(name, only+"/") {
			return errors.New("snapshot contains an unexpected root")
		}
		seen[name] = true
		count++
		if count > lim.MaxFiles {
			return errors.New("snapshot file count exceeded")
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA && hdr.Typeflag != tar.TypeDir {
			return fmt.Errorf("snapshot rejects nonregular entry %q", name)
		}
		if (name == "workspace" || name == "skill") && hdr.Typeflag != tar.TypeDir {
			return errors.New("snapshot top-level roots must be directories")
		}
		if hdr.Typeflag == tar.TypeDir {
			if hdr.Size != 0 {
				return errors.New("snapshot directory has data")
			}
			if err = mkdirInside(root, name); err != nil {
				return err
			}
			continue
		}
		if hdr.Size < 0 || hdr.Size > lim.MaxFileBytes || total+hdr.Size > lim.MaxBytes {
			return errors.New("snapshot content exceeds limit")
		}
		total += hdr.Size
		parent := path.Dir(name)
		if err = mkdirInside(root, parent); err != nil {
			return err
		}
		file, err := root.OpenFile(filepath.FromSlash(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(file, reader, hdr.Size)
		if copyErr == nil && hdr.Mode&0111 != 0 {
			copyErr = file.Chmod(0700)
		}
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if only != "" {
		if !seen[only] {
			return errors.New("snapshot missing required root")
		}
	} else if !seen["workspace"] || !seen["skill"] {
		return errors.New("snapshot missing workspace or skill root")
	}
	return nil
}

func mkdirInside(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	var current string
	for _, part := range strings.Split(name, "/") {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		local := filepath.FromSlash(current)
		if err := root.Mkdir(local, 0700); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return err
			}
			info, statErr := root.Lstat(local)
			if statErr != nil {
				return statErr
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("snapshot directory replaced with link")
			}
		}
	}
	return nil
}

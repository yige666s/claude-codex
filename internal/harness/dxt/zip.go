package dxt

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	maxZipFileSize  = 512 * 1024 * 1024
	maxZipTotalSize = 1024 * 1024 * 1024
	maxZipFileCount = 100000
)

func IsPathSafe(filePath string) bool {
	if filePath == "" {
		return false
	}
	if filepath.IsAbs(filePath) {
		return false
	}
	cleaned := filepath.Clean(filePath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false
	}
	return !strings.Contains(cleaned, "\x00")
}

func UnzipFile(zipData []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, err
	}

	out := make(map[string][]byte, len(reader.File))
	var totalSize uint64
	for index, file := range reader.File {
		if index+1 > maxZipFileCount {
			return nil, fmt.Errorf("archive contains too many files: %d", index+1)
		}
		if !IsPathSafe(file.Name) {
			return nil, fmt.Errorf("unsafe file path detected: %q", file.Name)
		}
		if file.UncompressedSize64 > maxZipFileSize {
			return nil, fmt.Errorf("file %q is too large", file.Name)
		}
		totalSize += file.UncompressedSize64
		if totalSize > maxZipTotalSize {
			return nil, fmt.Errorf("archive total size is too large")
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out[file.Name] = content
	}
	return out, nil
}

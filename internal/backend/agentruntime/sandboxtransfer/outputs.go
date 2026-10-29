package sandboxtransfer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"
)

// OutputFile is an in-memory, verified result. Persist it only after the
// caller has checked the complete set and bound it to an authenticated lease.
type OutputFile struct {
	Path    string
	SHA256  string
	Content []byte
}

// CollectOutputs consumes a tar stream from a stopped *command* running in a
// still-live isolated container (not docker cp of a tmpfs mount, which Docker
// does not support). It never extracts attacker-selected paths on the Node.
func CollectOutputs(ctx context.Context, input io.Reader, maxBytes int64, maxFiles int) ([]OutputFile, error) {
	if ctx == nil || input == nil || maxBytes < 1 || maxBytes > HardMaxBytes || maxFiles < 1 || maxFiles > HardMaxFiles {
		return nil, errors.New("invalid output collection limits")
	}
	// Account for tar headers/padding independently of content bytes.
	maxArchive := maxBytes + int64(maxFiles)*4096 + 2048
	raw, err := io.ReadAll(io.LimitReader(input, maxArchive+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxArchive {
		return nil, errors.New("output archive too large")
	}
	source := bytes.NewReader(raw)
	reader := tar.NewReader(source)
	seen := map[string]byte{}
	files := make([]OutputFile, 0)
	var total int64
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hdr, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name, err := validOutputPath(hdr.Name)
		if err != nil {
			return nil, err
		}
		if seen[name] != 0 {
			return nil, errors.New("duplicate output archive entry")
		}
		for ancestor := path.Dir(name); ancestor != "."; ancestor = path.Dir(ancestor) {
			if seen[ancestor] == 'f' {
				return nil, errors.New("output file used as directory")
			}
		}
		count++
		if count > maxFiles {
			return nil, errors.New("too many output entries")
		}
		if hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, errors.New("nonregular output rejected")
		}
		if name == "outputs" && hdr.Typeflag != tar.TypeDir {
			return nil, errors.New("output root must be a directory")
		}
		if hdr.Typeflag == tar.TypeDir {
			if hdr.Size != 0 {
				return nil, errors.New("output directory has content")
			}
			seen[name] = 'd'
			continue
		}
		if hdr.Size < 0 || hdr.Size > HardMaxFileBytes || hdr.Size > maxBytes-total {
			return nil, errors.New("output content limit exceeded")
		}
		contents := make([]byte, hdr.Size)
		if _, err := io.ReadFull(reader, contents); err != nil {
			return nil, err
		}
		total += hdr.Size
		sum := sha256.Sum256(contents)
		files = append(files, OutputFile{Path: strings.TrimPrefix(name, "outputs/"), SHA256: hex.EncodeToString(sum[:]), Content: contents})
		seen[name] = 'f'
	}
	// tar.Reader stops at its end marker. Reject nonzero bytes after that
	// marker rather than silently accepting a second concatenated payload.
	for _, b := range raw[len(raw)-source.Len():] {
		if b != 0 {
			return nil, errors.New("output archive contains trailing data")
		}
	}
	if seen["outputs"] != 'd' {
		return nil, errors.New("output root missing")
	}
	return files, nil
}
func validOutputPath(name string) (string, error) {
	clean := strings.TrimSuffix(name, "/")
	if clean == "" || len(clean) > 240 || strings.ContainsAny(clean, string([]byte{92, 58, 0})) || strings.HasPrefix(clean, "/") || path.Clean(clean) != clean || strings.Count(clean, "/") > 16 ||
		(clean != "outputs" && !strings.HasPrefix(clean, "outputs/")) {
		return "", errors.New("invalid output path")
	}
	for _, r := range clean {
		if r < 32 || r == 127 {
			return "", errors.New("output path contains control character")
		}
	}
	for _, component := range strings.Split(clean, "/") {
		if component == "" || component == "." || component == ".." {
			return "", errors.New("invalid output component")
		}
	}
	return clean, nil
}

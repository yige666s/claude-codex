package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/app/config"
)

type Manifest struct {
	Name       string                   `json:"name"`
	Version    string                   `json:"version,omitempty"`
	Path       string                   `json:"-"`
	MCPServers []config.MCPServerConfig `json:"mcp_servers,omitempty"`
}

type Loader struct {
	root string
}

func NewLoader(root string) *Loader {
	return &Loader{root: strings.TrimSpace(root)}
}

func (l *Loader) Load() ([]Manifest, error) {
	if l == nil || l.root == "" {
		return nil, nil
	}

	info, err := os.Stat(l.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var manifests []Manifest
	if !info.IsDir() {
		manifest, err := readManifest(l.root)
		if err != nil {
			return nil, err
		}
		return []Manifest{manifest}, nil
	}

	err = filepath.WalkDir(l.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != l.root && depth(l.root, path) > 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(entry.Name(), "plugin.json") {
			manifest, err := readManifest(path)
			if err != nil {
				return err
			}
			manifests = append(manifests, manifest)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return manifests, nil
}

func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	manifest.Path = path
	return manifest, nil
}

func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(filepath.ToSlash(rel), "/"))
}

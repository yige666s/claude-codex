package plugins

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Installer struct {
	CacheDir       string
	InstalledStore InstalledPluginStore
}

func (i Installer) InstallLocal(pluginID string, sourcePath string, scope PluginScope, projectPath string) (PluginInstallation, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return PluginInstallation{}, fmt.Errorf("plugin ID is required")
	}
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return PluginInstallation{}, fmt.Errorf("source path is required")
	}
	manifestPath := filepath.Join(sourcePath, "plugin.json")
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return PluginInstallation{}, err
	}
	parsed := ParsePluginIdentifier(pluginID)
	if parsed.Name != "" && parsed.Name != manifest.Name {
		return PluginInstallation{}, fmt.Errorf("plugin ID %q does not match manifest name %q", pluginID, manifest.Name)
	}

	cacheDir := i.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "claude-codex-plugins")
	}
	destination := filepath.Join(cacheDir, safeCacheName(pluginID), manifest.Version)
	if manifest.Version == "" {
		destination = filepath.Join(cacheDir, safeCacheName(pluginID), "unversioned")
	}
	if err := os.RemoveAll(destination); err != nil {
		return PluginInstallation{}, err
	}
	if err := copyDir(sourcePath, destination); err != nil {
		return PluginInstallation{}, err
	}

	registry, err := i.InstalledStore.Load()
	if err != nil {
		return PluginInstallation{}, err
	}
	info := PluginInstallation{
		PluginID:    pluginID,
		InstallPath: destination,
		Version:     manifest.Version,
		Scope:       scope,
		ProjectPath: projectPath,
	}
	registry.Upsert(info)
	info = registry.Plugins[pluginID]
	if err := i.InstalledStore.Save(registry); err != nil {
		return PluginInstallation{}, err
	}
	return info, nil
}

func (i Installer) Uninstall(pluginID string, settingsPath string) error {
	registry, err := i.InstalledStore.Load()
	if err != nil {
		return err
	}
	info, ok := registry.Plugins[pluginID]
	if ok {
		if err := os.RemoveAll(info.InstallPath); err != nil {
			return err
		}
		registry.Remove(pluginID)
		if err := i.InstalledStore.Save(registry); err != nil {
			return err
		}
	}
	if settingsPath != "" {
		if err := RemoveEnabledPluginSetting(settingsPath, pluginID); err != nil {
			return err
		}
	}
	return nil
}

func safeCacheName(pluginID string) string {
	replacer := strings.NewReplacer("@", "__", "/", "_", "\\", "_", "..", "_")
	return replacer.Replace(pluginID)
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

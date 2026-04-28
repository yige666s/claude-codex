package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type PluginScope string

const (
	PluginScopeUser    PluginScope = "user"
	PluginScopeProject PluginScope = "project"
	PluginScopeLocal   PluginScope = "local"
	PluginScopeManaged PluginScope = "managed"
	PluginScopeFlag    PluginScope = "flag"
)

type PluginInstallation struct {
	PluginID    string      `json:"pluginId"`
	InstallPath string      `json:"installPath"`
	Version     string      `json:"version,omitempty"`
	Scope       PluginScope `json:"scope,omitempty"`
	ProjectPath string      `json:"projectPath,omitempty"`
	InstalledAt string      `json:"installedAt,omitempty"`
	UpdatedAt   string      `json:"updatedAt,omitempty"`
}

type InstalledPluginRegistry struct {
	Version int                           `json:"version"`
	Plugins map[string]PluginInstallation `json:"plugins"`
}

type InstalledPluginStore struct {
	Path string
}

func NewInstalledPluginRegistry() InstalledPluginRegistry {
	return InstalledPluginRegistry{
		Version: 2,
		Plugins: make(map[string]PluginInstallation),
	}
}

func (r *InstalledPluginRegistry) Upsert(info PluginInstallation) {
	if r.Version == 0 {
		r.Version = 2
	}
	if r.Plugins == nil {
		r.Plugins = make(map[string]PluginInstallation)
	}
	if info.PluginID == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if info.Scope == "" {
		info.Scope = PluginScopeUser
	}
	if info.InstalledAt == "" {
		if existing, ok := r.Plugins[info.PluginID]; ok {
			info.InstalledAt = existing.InstalledAt
		}
		if info.InstalledAt == "" {
			info.InstalledAt = now
		}
	}
	info.UpdatedAt = now
	r.Plugins[info.PluginID] = info
}

func (r *InstalledPluginRegistry) Remove(pluginID string) bool {
	if r == nil || r.Plugins == nil {
		return false
	}
	if _, ok := r.Plugins[pluginID]; !ok {
		return false
	}
	delete(r.Plugins, pluginID)
	return true
}

func (s InstalledPluginStore) Load() (InstalledPluginRegistry, error) {
	if s.Path == "" {
		return NewInstalledPluginRegistry(), nil
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return NewInstalledPluginRegistry(), nil
	}
	if err != nil {
		return InstalledPluginRegistry{}, err
	}

	var registry InstalledPluginRegistry
	if err := json.Unmarshal(data, &registry); err == nil && registry.Plugins != nil {
		if registry.Version == 0 {
			registry.Version = 2
		}
		normalizeInstalledPlugins(&registry)
		return registry, nil
	}

	var legacy map[string]PluginInstallation
	if err := json.Unmarshal(data, &legacy); err != nil {
		return InstalledPluginRegistry{}, err
	}
	registry = NewInstalledPluginRegistry()
	for pluginID, info := range legacy {
		if info.PluginID == "" {
			info.PluginID = pluginID
		}
		if info.Scope == "" {
			info.Scope = PluginScopeUser
		}
		registry.Plugins[pluginID] = info
	}
	return registry, nil
}

func (s InstalledPluginStore) Save(registry InstalledPluginRegistry) error {
	if s.Path == "" {
		return nil
	}
	if registry.Version == 0 {
		registry.Version = 2
	}
	if registry.Plugins == nil {
		registry.Plugins = map[string]PluginInstallation{}
	}
	normalizeInstalledPlugins(&registry)
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Path, append(data, '\n'), 0o644)
}

func normalizeInstalledPlugins(registry *InstalledPluginRegistry) {
	for pluginID, info := range registry.Plugins {
		if info.PluginID == "" {
			info.PluginID = pluginID
		}
		if info.Scope == "" {
			info.Scope = PluginScopeUser
		}
		registry.Plugins[pluginID] = info
	}
}

func LoadInstalledPlugins(registry InstalledPluginRegistry, enabledPlugins map[string]bool) ([]*LoadedPlugin, error) {
	ids := make([]string, 0, len(registry.Plugins))
	for pluginID := range registry.Plugins {
		ids = append(ids, pluginID)
	}
	sort.Strings(ids)
	var loaded []*LoadedPlugin
	for _, pluginID := range ids {
		info := registry.Plugins[pluginID]
		if info.Scope == PluginScopeFlag {
			continue
		}
		parsed := ParsePluginIdentifier(pluginID)
		marketplace := parsed.Marketplace
		if marketplace == "" {
			marketplace = InlineMarketplaceName
		}
		plugins, err := NewLoader(filepath.Join(info.InstallPath, "plugin.json")).LoadDetailed(LoadOptions{
			Marketplace:     marketplace,
			Repository:      marketplace,
			EnabledPlugins:  enabledPlugins,
			IncludeDisabled: true,
		})
		if err != nil {
			return nil, fmt.Errorf("load installed plugin %s: %w", pluginID, err)
		}
		for _, plugin := range plugins {
			if plugin.Source == pluginID {
				loaded = append(loaded, plugin)
			}
		}
	}
	return loaded, nil
}

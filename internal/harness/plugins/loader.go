package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const InlineMarketplaceName = "inline"

type PluginAuthor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

func (a *PluginAuthor) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		a.Name = name
		return nil
	}
	type alias PluginAuthor
	var parsed alias
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*a = PluginAuthor(parsed)
	return nil
}

type CommandMetadata struct {
	Source       string   `json:"source,omitempty"`
	Content      string   `json:"content,omitempty"`
	Description  string   `json:"description,omitempty"`
	ArgumentHint string   `json:"argumentHint,omitempty"`
	Model        string   `json:"model,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
}

type CommandDefinitions struct {
	Paths    []string                   `json:"paths,omitempty"`
	Metadata map[string]CommandMetadata `json:"metadata,omitempty"`
}

func (c *CommandDefinitions) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		c.Paths = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		c.Paths = list
		return nil
	}
	var metadata map[string]CommandMetadata
	if err := json.Unmarshal(data, &metadata); err == nil {
		c.Metadata = metadata
		return nil
	}
	return fmt.Errorf("commands must be a path, path list, or command metadata map")
}

type PathList []string

func (p *PathList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*p = []string{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*p = list
		return nil
	}
	return fmt.Errorf("value must be a path or path list")
}

type Manifest struct {
	Name         string             `json:"name"`
	Version      string             `json:"version,omitempty"`
	Description  string             `json:"description,omitempty"`
	Author       *PluginAuthor      `json:"author,omitempty"`
	License      string             `json:"license,omitempty"`
	Homepage     string             `json:"homepage,omitempty"`
	Repository   string             `json:"repository,omitempty"`
	Keywords     []string           `json:"keywords,omitempty"`
	Dependencies []string           `json:"dependencies,omitempty"`
	Commands     CommandDefinitions `json:"commands,omitempty"`
	Agents       PathList           `json:"agents,omitempty"`
	Skills       PathList           `json:"skills,omitempty"`
	OutputStyles PathList           `json:"outputStyles,omitempty"`
	Hooks        any                `json:"hooks,omitempty"`
	MCPServers   map[string]any     `json:"mcpServers,omitempty"`
	LSPServers   map[string]any     `json:"lspServers,omitempty"`
	Settings     map[string]any     `json:"settings,omitempty"`
	Path         string             `json:"-"`
	Root         string             `json:"-"`
}

func (m *Manifest) UnmarshalJSON(data []byte) error {
	type manifestAlias Manifest
	var raw struct {
		manifestAlias
		MCPServersSnake map[string]any `json:"mcp_servers,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = Manifest(raw.manifestAlias)
	if m.MCPServers == nil && raw.MCPServersSnake != nil {
		m.MCPServers = raw.MCPServersSnake
	}
	return nil
}

type LoadOptions struct {
	Marketplace     string
	Repository      string
	EnabledPlugins  map[string]bool
	IncludeDisabled bool
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

	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Path < manifests[j].Path
	})
	return manifests, nil
}

func (l *Loader) LoadDetailed(opts LoadOptions) ([]*LoadedPlugin, error) {
	manifests, err := l.Load()
	if err != nil {
		return nil, err
	}
	marketplace := strings.TrimSpace(opts.Marketplace)
	if marketplace == "" {
		marketplace = InlineMarketplaceName
	}
	repository := strings.TrimSpace(opts.Repository)
	if repository == "" {
		repository = marketplace
	}

	seen := make(map[string]string, len(manifests))
	loaded := make([]*LoadedPlugin, 0, len(manifests))
	for _, manifest := range manifests {
		pluginID := BuildPluginID(manifest.Name, marketplace)
		if previous, ok := seen[pluginID]; ok {
			return nil, fmt.Errorf("duplicate plugin %q in %s and %s", pluginID, previous, manifest.Path)
		}
		seen[pluginID] = manifest.Path

		enabled := true
		if opts.EnabledPlugins != nil {
			if value, ok := opts.EnabledPlugins[pluginID]; ok {
				enabled = value
			}
		}
		if !enabled && !opts.IncludeDisabled {
			continue
		}

		plugin := manifest.toLoadedPlugin(pluginID, repository, enabled)
		loaded = append(loaded, plugin)
	}
	return loaded, nil
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
	manifest.Root = filepath.Dir(path)
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("plugin name is required")
	}
	if strings.ContainsAny(m.Name, " \t\r\n") {
		return fmt.Errorf("plugin name %q must not contain whitespace", m.Name)
	}
	if m.Root == "" && m.Path != "" {
		m.Root = filepath.Dir(m.Path)
	}
	root := m.Root
	for i, path := range m.Commands.Paths {
		if _, err := validateRelativePluginPath(root, path, fmt.Sprintf("commands[%d]", i)); err != nil {
			return err
		}
	}
	for name, command := range m.Commands.Metadata {
		hasSource := strings.TrimSpace(command.Source) != ""
		hasContent := strings.TrimSpace(command.Content) != ""
		if hasSource == hasContent {
			return fmt.Errorf("commands.%s must have exactly one of source or content", name)
		}
		if hasSource {
			if _, err := validateRelativePluginPath(root, command.Source, "commands."+name+".source"); err != nil {
				return err
			}
		}
	}
	if err := validatePathList(root, "agents", m.Agents); err != nil {
		return err
	}
	if err := validatePathList(root, "skills", m.Skills); err != nil {
		return err
	}
	if err := validatePathList(root, "outputStyles", m.OutputStyles); err != nil {
		return err
	}
	if err := validateHookReference(root, m.Hooks); err != nil {
		return err
	}
	return nil
}

func (m Manifest) toLoadedPlugin(pluginID string, repository string, enabled bool) *LoadedPlugin {
	root := m.Root
	plugin := &LoadedPlugin{
		Name:        m.Name,
		Manifest:    m.toPluginManifest(),
		Path:        root,
		Source:      pluginID,
		Repository:  repository,
		Enabled:     enabled,
		IsBuiltin:   false,
		HooksConfig: asMap(m.Hooks),
		MCPServers:  m.MCPServers,
		LSPServers:  m.LSPServers,
		Settings:    m.Settings,
	}
	plugin.CommandsPath = existingComponentPath(root, "commands")
	plugin.CommandsPaths = resolvePaths(root, m.Commands.Paths)
	plugin.CommandsMetadata = copyCommandMetadata(m.Commands.Metadata)
	plugin.AgentsPath = existingComponentPath(root, "agents")
	plugin.AgentsPaths = resolvePaths(root, m.Agents)
	plugin.SkillsPath = existingComponentPath(root, "skills")
	plugin.SkillsPaths = resolvePaths(root, m.Skills)
	plugin.OutputStylesPath = existingComponentPath(root, "output-styles")
	plugin.OutputStylesPaths = resolvePaths(root, m.OutputStyles)
	return plugin
}

func (m Manifest) toPluginManifest() PluginManifest {
	return PluginManifest{
		Name:         m.Name,
		Description:  m.Description,
		Version:      m.Version,
		Author:       m.Author,
		License:      m.License,
		Homepage:     m.Homepage,
		Repository:   m.Repository,
		Keywords:     append([]string(nil), m.Keywords...),
		Dependencies: append([]string(nil), m.Dependencies...),
	}
}

func validatePathList(root, field string, paths []string) error {
	for i, path := range paths {
		if _, err := validateRelativePluginPath(root, path, fmt.Sprintf("%s[%d]", field, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateHookReference(root string, hooks any) error {
	switch value := hooks.(type) {
	case nil:
		return nil
	case string:
		_, err := validateRelativePluginPath(root, value, "hooks")
		return err
	case []any:
		for i, item := range value {
			if path, ok := item.(string); ok {
				if _, err := validateRelativePluginPath(root, path, fmt.Sprintf("hooks[%d]", i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateRelativePluginPath(root, value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s path is required", field)
	}
	if !strings.HasPrefix(value, "./") {
		return "", fmt.Errorf("%s path %q must start with ./", field, value)
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("%s path %q must be relative", field, value)
	}
	resolved := filepath.Clean(filepath.Join(root, value))
	base := filepath.Clean(root)
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
		return "", fmt.Errorf("%s path %q escapes plugin root", field, value)
	}
	return resolved, nil
}

func existingComponentPath(root, name string) string {
	path := filepath.Join(root, name)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return ""
}

func resolvePaths(root string, paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		value, err := validateRelativePluginPath(root, path, "path")
		if err == nil {
			resolved = append(resolved, value)
		}
	}
	return resolved
}

func copyCommandMetadata(in map[string]CommandMetadata) map[string]CommandMetadata {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]CommandMetadata, len(in))
	for name, metadata := range in {
		out[name] = metadata
	}
	return out
}

func asMap(value any) map[string]interface{} {
	if value == nil {
		return nil
	}
	if mapped, ok := value.(map[string]interface{}); ok {
		return mapped
	}
	return map[string]interface{}{"value": value}
}

func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(filepath.ToSlash(rel), "/"))
}

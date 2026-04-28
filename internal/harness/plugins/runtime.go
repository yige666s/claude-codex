package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/agent"
	"claude-codex/internal/harness/hooks"
	"claude-codex/internal/harness/skills"
)

type RuntimeOptions struct {
	Plugins        []*LoadedPlugin
	SkillManager   *skills.SkillManager
	HookRegistry   *hooks.Registry
	RegisterAgents bool
}

type RuntimeReport struct {
	PluginsLoaded    int
	CommandsLoaded   int
	SkillsLoaded     int
	AgentsLoaded     int
	HooksLoaded      int
	MCPServersLoaded int
	Warnings         []string
}

func LoadRuntimeComponents(opts RuntimeOptions) (RuntimeReport, error) {
	report := RuntimeReport{}
	var pluginAgents []*agent.AgentDefinition

	for _, plugin := range opts.Plugins {
		if plugin == nil || !plugin.Enabled {
			continue
		}
		report.PluginsLoaded++
		if opts.SkillManager != nil {
			commands, err := loadPluginCommands(plugin)
			if err != nil {
				return report, err
			}
			report.CommandsLoaded += len(commands)
			report.SkillsLoaded += len(commands)
			if err := opts.SkillManager.RegisterLoadedSkills(commands); err != nil {
				return report, err
			}

			loadedSkills, err := loadPluginSkills(plugin)
			if err != nil {
				return report, err
			}
			report.SkillsLoaded += len(loadedSkills)
			if err := opts.SkillManager.RegisterLoadedSkills(loadedSkills); err != nil {
				return report, err
			}
		}
		agents, warnings, err := loadPluginAgents(plugin)
		if err != nil {
			return report, err
		}
		report.AgentsLoaded += len(agents)
		report.Warnings = append(report.Warnings, warnings...)
		pluginAgents = append(pluginAgents, agents...)

		if opts.HookRegistry != nil {
			count, err := RegisterPluginHooks(opts.HookRegistry, plugin)
			if err != nil {
				return report, err
			}
			report.HooksLoaded += count
		}
		report.MCPServersLoaded += len(MCPServerConfigs([]*LoadedPlugin{plugin}))
	}
	if opts.RegisterAgents {
		agent.RegisterPluginDefinitions(pluginAgents)
	}
	return report, nil
}

func loadPluginCommands(plugin *LoadedPlugin) ([]*skills.SkillDefinition, error) {
	loader := skills.NewSkillLoader()
	var out []*skills.SkillDefinition
	if plugin.CommandsPath != "" {
		loaded, err := loader.LoadCommandsFromDirectory(plugin.CommandsPath, skills.SourcePlugin)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded...)
	}
	for _, path := range plugin.CommandsPaths {
		loaded, err := loadPluginCommandPath(loader, path)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded...)
	}
	for name, metadata := range plugin.CommandsMetadata {
		skill, err := buildCommandMetadataSkill(plugin, name, metadata)
		if err != nil {
			return nil, err
		}
		out = append(out, skill)
	}
	prefixSkills(plugin, out)
	return out, nil
}

func loadPluginCommandPath(loader *skills.SkillLoader, path string) ([]*skills.SkillDefinition, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return loader.LoadCommandsFromDirectory(path, skills.SourcePlugin)
	}
	skill, err := loader.LoadSkillFromFile(path, skills.SourcePlugin)
	if err != nil {
		return nil, err
	}
	return []*skills.SkillDefinition{skill}, nil
}

func buildCommandMetadataSkill(plugin *LoadedPlugin, name string, metadata CommandMetadata) (*skills.SkillDefinition, error) {
	skillName := strings.TrimSpace(name)
	if skillName == "" {
		return nil, fmt.Errorf("plugin %s has command metadata with empty name", plugin.Name)
	}
	content := metadata.Content
	sourcePath := ""
	if strings.TrimSpace(metadata.Source) != "" {
		resolved, err := validateRelativePluginPath(plugin.Path, metadata.Source, "commands."+name+".source")
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, err
		}
		content = string(data)
		sourcePath = resolved
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("plugin %s command %s has empty content", plugin.Name, name)
	}
	description := metadata.Description
	if description == "" {
		description = "Plugin command"
	}
	skill := &skills.SkillDefinition{
		Name:          skillName,
		Description:   description,
		ArgumentHint:  metadata.ArgumentHint,
		AllowedTools:  append([]string(nil), metadata.AllowedTools...),
		Model:         metadata.Model,
		UserInvocable: true,
		Source:        skills.SourcePlugin,
		LoadedFrom:    string(skills.LoadedFromCommands),
		Content:       content,
		ContentLength: len(content),
		SkillRoot:     plugin.Path,
		FileIdentity:  sourcePath,
		LoadedAt:      time.Now(),
	}
	skill.GetPrompt = func(args string, _ *skills.SkillContext) ([]skills.ContentBlock, error) {
		return []skills.ContentBlock{{Type: "text", Text: strings.ReplaceAll(content, "$ARGUMENTS", args)}}, nil
	}
	return skill, nil
}

func loadPluginSkills(plugin *LoadedPlugin) ([]*skills.SkillDefinition, error) {
	loader := skills.NewSkillLoader()
	var out []*skills.SkillDefinition
	if plugin.SkillsPath != "" {
		loaded, err := loader.LoadSkillsFromDirectory(plugin.SkillsPath, skills.SourcePlugin)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded...)
	}
	for _, path := range plugin.SkillsPaths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		var loaded []*skills.SkillDefinition
		if info.IsDir() {
			loaded, err = loader.LoadSkillsFromDirectory(path, skills.SourcePlugin)
		} else {
			var skill *skills.SkillDefinition
			skill, err = loader.LoadSkillFromFile(path, skills.SourcePlugin)
			if skill != nil {
				loaded = []*skills.SkillDefinition{skill}
			}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, loaded...)
	}
	prefixSkills(plugin, out)
	return out, nil
}

func prefixSkills(plugin *LoadedPlugin, skillDefs []*skills.SkillDefinition) {
	for _, skill := range skillDefs {
		if skill == nil {
			continue
		}
		if !strings.HasPrefix(skill.Name, plugin.Name+":") {
			skill.Name = plugin.Name + ":" + skill.Name
		}
		skill.Source = skills.SourcePlugin
		skill.SkillRoot = plugin.Path
	}
}

func loadPluginAgents(plugin *LoadedPlugin) ([]*agent.AgentDefinition, []string, error) {
	var out []*agent.AgentDefinition
	var warnings []string
	if plugin.AgentsPath != "" {
		loaded, warn, err := agent.LoadDefinitionsFromDirectory(plugin.AgentsPath, agent.SourcePlugin)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, loaded...)
		warnings = append(warnings, warn...)
	}
	for _, path := range plugin.AgentsPaths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil, err
		}
		if info.IsDir() {
			loaded, warn, err := agent.LoadDefinitionsFromDirectory(path, agent.SourcePlugin)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, loaded...)
			warnings = append(warnings, warn...)
			continue
		}
		def, warn, err := agent.LoadDefinitionFromFile(path, agent.SourcePlugin)
		if err != nil {
			return nil, nil, err
		}
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if def != nil {
			out = append(out, def)
		}
	}
	for _, def := range out {
		if def == nil {
			continue
		}
		if !strings.HasPrefix(string(def.AgentType), plugin.Name+":") {
			def.AgentType = agent.AgentType(plugin.Name + ":" + string(def.AgentType))
		}
		def.Source = agent.SourcePlugin
		if def.BaseDir == "" {
			def.BaseDir = plugin.Path
		}
	}
	return out, warnings, nil
}

func MCPServerConfigs(pluginList []*LoadedPlugin) []config.MCPServerConfig {
	var configs []config.MCPServerConfig
	for _, plugin := range pluginList {
		if plugin == nil || !plugin.Enabled {
			continue
		}
		names := make([]string, 0, len(plugin.MCPServers))
		for name := range plugin.MCPServers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			cfg, ok := mcpConfigFromMap(name, plugin.MCPServers[name])
			if !ok {
				continue
			}
			cfg.Name = plugin.Name + ":" + cfg.Name
			configs = append(configs, cfg)
		}
	}
	return configs
}

func mcpConfigFromMap(name string, value any) (config.MCPServerConfig, bool) {
	cfg := config.MCPServerConfig{Name: name}
	mapped, ok := value.(map[string]any)
	if !ok {
		return cfg, false
	}
	if transport, _ := mapped["transport"].(string); transport != "" {
		cfg.Transport = transport
	} else if typ, _ := mapped["type"].(string); typ != "" {
		cfg.Transport = typ
	}
	if cfg.Transport == "" {
		cfg.Transport = "stdio"
	}
	if command, ok := mapped["command"].(string); ok && command != "" {
		cfg.Command = []string{command}
	} else if command, ok := mapped["command"].([]any); ok {
		for _, item := range command {
			if s, ok := item.(string); ok {
				cfg.Command = append(cfg.Command, s)
			}
		}
	}
	if args, ok := mapped["args"].([]any); ok {
		for _, item := range args {
			if s, ok := item.(string); ok {
				cfg.Command = append(cfg.Command, s)
			}
		}
	}
	if url, _ := mapped["url"].(string); url != "" {
		cfg.URL = url
	}
	if headers, ok := mapped["headers"].(map[string]any); ok {
		cfg.Headers = make(map[string]string, len(headers))
		for key, value := range headers {
			if s, ok := value.(string); ok {
				cfg.Headers[key] = s
			}
		}
	}
	return cfg, true
}

func RegisterPluginHooks(registry *hooks.Registry, plugin *LoadedPlugin) (int, error) {
	if registry == nil || plugin == nil || len(plugin.HooksConfig) == 0 {
		return 0, nil
	}
	count := 0
	for eventName, rawMatchers := range plugin.HooksConfig {
		event := hooks.HookEvent(eventName)
		matchers, ok := rawMatchers.([]any)
		if !ok {
			continue
		}
		for matcherIndex, rawMatcher := range matchers {
			matcherMap, ok := rawMatcher.(map[string]any)
			if !ok {
				continue
			}
			matcher, _ := matcherMap["matcher"].(string)
			rawHooks, _ := matcherMap["hooks"].([]any)
			for hookIndex, rawHook := range rawHooks {
				hookMap, ok := rawHook.(map[string]any)
				if !ok {
					continue
				}
				if hookMap["type"] != "command" {
					continue
				}
				command, _ := hookMap["command"].(string)
				if strings.TrimSpace(command) == "" {
					return count, fmt.Errorf("plugin %s hook %s[%d].hooks[%d] command is required", plugin.Name, event, matcherIndex, hookIndex)
				}
				timeout := hooks.DefaultTimeout
				if seconds, ok := hookMap["timeout"].(float64); ok && seconds > 0 {
					timeout = time.Duration(seconds * float64(time.Second))
				}
				async, _ := hookMap["async"].(bool)
				if rewake, _ := hookMap["asyncRewake"].(bool); rewake {
					async = true
				}
				if err := registry.Register(&PluginCommandHook{
					name:     fmt.Sprintf("%s:%s:%d:%d", plugin.Name, event, matcherIndex, hookIndex),
					event:    event,
					matcher:  matcher,
					command:  command,
					root:     plugin.Path,
					plugin:   plugin.Name,
					pluginID: plugin.Source,
					timeout:  timeout,
					async:    async,
				}); err != nil {
					return count, err
				}
				count++
			}
		}
	}
	return count, nil
}

type PluginCommandHook struct {
	name     string
	event    hooks.HookEvent
	matcher  string
	command  string
	root     string
	plugin   string
	pluginID string
	timeout  time.Duration
	async    bool
}

func (h *PluginCommandHook) Name() string           { return h.name }
func (h *PluginCommandHook) Event() hooks.HookEvent { return h.event }
func (h *PluginCommandHook) IsAsync() bool          { return h.async }
func (h *PluginCommandHook) Timeout() time.Duration { return h.timeout }

func (h *PluginCommandHook) Execute(ctx context.Context, input *hooks.HookInput) (*hooks.HookResult, error) {
	if !h.matches(input) {
		return &hooks.HookResult{Continue: true}, nil
	}
	payload, _ := json.Marshal(input)
	cmd := exec.CommandContext(ctx, shellName(), shellArg(), h.command)
	cmd.Dir = h.root
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"CLAUDE_PLUGIN_ROOT="+h.root,
		"CLAUDE_PLUGIN_NAME="+h.plugin,
		"CLAUDE_PLUGIN_ID="+h.pluginID,
	)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return &hooks.HookResult{Continue: true, BlockingError: strings.TrimSpace(err.Error() + "\n" + text)}, nil
	}
	return &hooks.HookResult{Continue: true, AdditionalContext: text}, nil
}

func (h *PluginCommandHook) matches(input *hooks.HookInput) bool {
	matcher := strings.TrimSpace(h.matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	if input == nil || input.Tool == nil {
		return false
	}
	return input.Tool.Name == matcher || strings.Contains(input.Tool.Name, matcher)
}

func shellName() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func shellArg() string {
	return "-c"
}

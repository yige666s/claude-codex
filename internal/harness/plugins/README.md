# Plugins module

The plugins package mirrors the TypeScript plugin core in Go: manifest loading,
plugin identifiers, installed plugin state, known marketplaces, local install
management, built-in plugins, and runtime component loading.

## Supported runtime surface

- TS-style `plugin.json` metadata.
- `commands`, including default `commands/`, extra paths, and inline metadata.
- `skills`, including default `skills/` and extra paths.
- `agents`, including default `agents/` and extra paths.
- `hooks` command hooks with plugin context environment.
- `mcpServers` conversion into existing Go MCP server config.
- `lspServers` and `settings` retained on `LoadedPlugin` for consumers.
- `enabledPlugins` overrides.
- `installed_plugins.json` v2 plus legacy map migration.
- `known_marketplaces.json` with official-name protection.
- Local install/uninstall into a plugin cache.

## Key entry points

```go
loaded, err := plugins.NewLoader(pluginDir).LoadDetailed(plugins.LoadOptions{
    Marketplace: plugins.InlineMarketplaceName,
    Repository:  "plugin_dir",
})

report, err := plugins.LoadRuntimeComponents(plugins.RuntimeOptions{
    Plugins:        loaded,
    SkillManager:   skillManager,
    HookRegistry:   hookRegistry,
    RegisterAgents: true,
})
```

`LoadRuntimeComponents` registers plugin commands and skills into the provided
`SkillManager`, registers plugin agents with the agent definition cache, converts
plugin MCP servers, and registers command hooks when a hook registry is provided.

## Remaining differences from TypeScript

- Remote marketplace network/git install and autoupdate are still represented by
  local/cache primitives rather than a full fetch/update pipeline.
- The full `/plugin` UI/CLI management matrix is not yet ported.
- Prompt, HTTP, and agent hooks are preserved in manifest data but only command
  hooks have a native Go executor today.

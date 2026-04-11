package skills

// MCP (Model Context Protocol) skill integration
// This module provides a bridge for loading skills from MCP servers

// MCPSkillBuilder is a function that creates a skill from MCP tool metadata
type MCPSkillBuilder func(toolName string, metadata map[string]interface{}) (*SkillDefinition, error)

// MCPSkillRegistry manages MCP skill builders
type MCPSkillRegistry struct {
	builder MCPSkillBuilder
}

var mcpRegistry = &MCPSkillRegistry{}

// RegisterMCPSkillBuilder registers a builder function for MCP skills
// This should be called during application initialization
func RegisterMCPSkillBuilder(builder MCPSkillBuilder) {
	mcpRegistry.builder = builder
}

// GetMCPSkillBuilder returns the registered MCP skill builder
func GetMCPSkillBuilder() MCPSkillBuilder {
	return mcpRegistry.builder
}

// BuildMCPSkill builds a skill from MCP tool metadata
func BuildMCPSkill(toolName string, metadata map[string]interface{}) (*SkillDefinition, error) {
	if mcpRegistry.builder == nil {
		return nil, &SkillError{
			Message: "MCP skill builder not registered",
		}
	}

	return mcpRegistry.builder(toolName, metadata)
}

// DefaultMCPSkillBuilder is a default implementation of MCPSkillBuilder
func DefaultMCPSkillBuilder(toolName string, metadata map[string]interface{}) (*SkillDefinition, error) {
	// Extract metadata fields
	description := ""
	if desc, ok := metadata["description"].(string); ok {
		description = desc
	}

	inputSchema := metadata["inputSchema"]

	// Create skill
	skill := &SkillDefinition{
		Name:                        toolName,
		Description:                 description,
		HasUserSpecifiedDescription: description != "",
		Source:                      SourceMCP,
		LoadedFrom:                  "mcp",
		UserInvocable:               true,
		AllowedTools:                []string{}, // MCP skills can use all tools by default
	}

	// Create prompt generator that describes the MCP tool
	skill.GetPrompt = func(args string, ctx *SkillContext) ([]ContentBlock, error) {
		prompt := "Use the MCP tool: " + toolName + "\n\n"
		if description != "" {
			prompt += "Description: " + description + "\n\n"
		}
		if inputSchema != nil {
			prompt += "This tool accepts structured input.\n\n"
		}
		if args != "" {
			prompt += "Arguments: " + args + "\n"
		}

		return []ContentBlock{{Type: "text", Text: prompt}}, nil
	}

	return skill, nil
}

// LoadMCPSkills loads skills from MCP server metadata
func (m *SkillManager) LoadMCPSkills(tools []map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tool := range tools {
		// Extract tool name
		toolName, ok := tool["name"].(string)
		if !ok {
			continue
		}

		// Build skill
		skill, err := BuildMCPSkill(toolName, tool)
		if err != nil {
			continue
		}

		// Register skill
		if err := m.registry.Register(skill); err != nil {
			// Skip duplicates
			continue
		}

		m.dynamicSkills[skill.Name] = skill
	}

	m.notifyListeners()
	return nil
}

// RemoveMCPSkills removes all MCP skills
func (m *SkillManager) RemoveMCPSkills() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find and remove MCP skills
	for name, skill := range m.dynamicSkills {
		if skill.Source == SourceMCP {
			m.registry.Remove(name)
			delete(m.dynamicSkills, name)
		}
	}

	m.notifyListeners()
}

// GetMCPSkills returns all MCP skills
func (m *SkillManager) GetMCPSkills() []*SkillDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var mcpSkills []*SkillDefinition
	for _, skill := range m.dynamicSkills {
		if skill.Source == SourceMCP {
			mcpSkills = append(mcpSkills, skill)
		}
	}

	return mcpSkills
}

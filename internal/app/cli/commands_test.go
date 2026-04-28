package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDefRegistry_Register(t *testing.T) {
	registry := NewCommandDefRegistry()

	cmd := &CommandDef{
		Name:        "test",
		Description: "Test command",
		Type:        CommandTypeBuiltin,
		Aliases:     []string{"t", "tst"},
	}

	err := registry.Register(cmd)
	require.NoError(t, err)

	// Verify command is registered
	found := registry.Find("test")
	assert.NotNil(t, found)
	assert.Equal(t, "test", found.Name)

	// Verify aliases work
	assert.NotNil(t, registry.Find("t"))
	assert.NotNil(t, registry.Find("tst"))
}

func TestCommandDefRegistry_Register_Duplicate(t *testing.T) {
	registry := NewCommandDefRegistry()

	cmd1 := &CommandDef{Name: "test", Description: "Test 1", Type: CommandTypeBuiltin}
	cmd2 := &CommandDef{Name: "test", Description: "Test 2", Type: CommandTypeBuiltin}

	err := registry.Register(cmd1)
	require.NoError(t, err)

	err = registry.Register(cmd2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestCommandDefRegistry_Find(t *testing.T) {
	registry := NewCommandDefRegistry()

	cmd := &CommandDef{
		Name:    "commit",
		Aliases: []string{"c", "ci"},
		Type:    CommandTypeBuiltin,
	}
	registry.Register(cmd)

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{"by name", "commit", true},
		{"by alias 1", "c", true},
		{"by alias 2", "ci", true},
		{"not found", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := registry.Find(tt.query)
			if tt.expected {
				assert.NotNil(t, found)
				assert.Equal(t, "commit", found.Name)
			} else {
				assert.Nil(t, found)
			}
		})
	}
}

func TestCommandDefRegistry_List(t *testing.T) {
	registry := NewCommandDefRegistry()

	registry.Register(&CommandDef{Name: "commit", Type: CommandTypeBuiltin})
	registry.Register(&CommandDef{Name: "branch", Type: CommandTypeBuiltin})
	registry.Register(&CommandDef{Name: "diff", Type: CommandTypeBuiltin})

	commands := registry.List()
	assert.Len(t, commands, 3)

	// Should be sorted by name
	assert.Equal(t, "branch", commands[0].Name)
	assert.Equal(t, "commit", commands[1].Name)
	assert.Equal(t, "diff", commands[2].Name)
}

func TestCommandDefRegistry_ListVisible(t *testing.T) {
	registry := NewCommandDefRegistry()

	registry.Register(&CommandDef{Name: "visible1", Type: CommandTypeBuiltin, Hidden: false})
	registry.Register(&CommandDef{Name: "hidden", Type: CommandTypeBuiltin, Hidden: true})
	registry.Register(&CommandDef{Name: "visible2", Type: CommandTypeBuiltin, Hidden: false})

	visible := registry.ListVisible()
	assert.Len(t, visible, 2)
	assert.Equal(t, "visible1", visible[0].Name)
	assert.Equal(t, "visible2", visible[1].Name)
}

func TestCommandDefRegistry_ListBySource(t *testing.T) {
	registry := NewCommandDefRegistry()

	registry.Register(&CommandDef{Name: "builtin1", Source: CommandSourceBuiltin})
	registry.Register(&CommandDef{Name: "plugin1", Source: CommandSourcePlugin})
	registry.Register(&CommandDef{Name: "builtin2", Source: CommandSourceBuiltin})

	builtins := registry.ListBySource(CommandSourceBuiltin)
	assert.Len(t, builtins, 2)

	plugins := registry.ListBySource(CommandSourcePlugin)
	assert.Len(t, plugins, 1)
}

func TestCommandDefRegistry_ListByType(t *testing.T) {
	registry := NewCommandDefRegistry()

	registry.Register(&CommandDef{Name: "cmd1", Type: CommandTypeBuiltin})
	registry.Register(&CommandDef{Name: "cmd2", Type: CommandTypePrompt})
	registry.Register(&CommandDef{Name: "cmd3", Type: CommandTypeBuiltin})

	builtins := registry.ListByType(CommandTypeBuiltin)
	assert.Len(t, builtins, 2)

	prompts := registry.ListByType(CommandTypePrompt)
	assert.Len(t, prompts, 1)
}

func TestCommandDefRegistry_Unregister(t *testing.T) {
	registry := NewCommandDefRegistry()

	cmd := &CommandDef{
		Name:    "test",
		Aliases: []string{"t"},
		Type:    CommandTypeBuiltin,
	}
	registry.Register(cmd)

	assert.True(t, registry.Has("test"))
	assert.True(t, registry.Has("t"))

	err := registry.Unregister("test")
	require.NoError(t, err)

	assert.False(t, registry.Has("test"))
	assert.False(t, registry.Has("t"))
}

func TestFormatDescriptionWithSource(t *testing.T) {
	tests := []struct {
		name     string
		cmd      *CommandDef
		expected string
	}{
		{
			name:     "builtin command",
			cmd:      &CommandDef{Description: "Test", Type: CommandTypeBuiltin},
			expected: "Test",
		},
		{
			name:     "workflow",
			cmd:      &CommandDef{Description: "Test", Type: CommandTypePrompt, Kind: CommandKindWorkflow},
			expected: "Test (workflow)",
		},
		{
			name:     "plugin",
			cmd:      &CommandDef{Description: "Test", Type: CommandTypePrompt, Source: CommandSourcePlugin},
			expected: "Test (plugin)",
		},
		{
			name:     "bundled",
			cmd:      &CommandDef{Description: "Test", Type: CommandTypePrompt, Source: CommandSourceBundled},
			expected: "Test (bundled)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDescriptionWithSource(tt.cmd)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedCmd  string
		expectedArgs []string
	}{
		{
			name:         "simple command",
			input:        "commit",
			expectedCmd:  "commit",
			expectedArgs: []string{},
		},
		{
			name:         "command with slash",
			input:        "/commit",
			expectedCmd:  "commit",
			expectedArgs: []string{},
		},
		{
			name:         "command with args",
			input:        "commit -m 'test message'",
			expectedCmd:  "commit",
			expectedArgs: []string{"-m", "'test", "message'"},
		},
		{
			name:         "empty input",
			input:        "",
			expectedCmd:  "",
			expectedArgs: nil,
		},
		{
			name:         "whitespace only",
			input:        "   ",
			expectedCmd:  "",
			expectedArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := ParseCommandLine(tt.input)
			assert.Equal(t, tt.expectedCmd, cmd)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

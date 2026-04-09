# Command Autocomplete Implementation

## Overview
Implemented command autocomplete functionality for the Go TUI, allowing users to see command suggestions when typing "/" in the input field. **Slash commands are now executed locally instead of being sent to the LLM.**

## Features Implemented

### 1. Command Suggestion System (`internal/tui/suggestions.go`)
- **Fuzzy matching algorithm**: Matches commands by prefix, contains, and fuzzy patterns
- **Scoring system**: Ranks suggestions by relevance
  - Exact match: 1000 points
  - Prefix match: 500 points
  - Contains match: 200 points
  - Alias matches: 900/400/150 points
  - Description match: 50 points
  - Fuzzy match bonus: 100 points
- **Configurable result limit**: Returns top N suggestions

### 2. TUI Integration (`internal/tui/model.go`)
- Added `suggestions` and `suggestionIndex` fields to track current suggestions
- Added `registry` field to access command registry
- **Keyboard navigation**:
  - **Up/Down arrows**: Navigate through suggestions (when suggestions are visible)
  - **Tab**: Accept current suggestion and insert into input
  - **Escape**: Clear suggestions (via normal mode)
- **Real-time updates**: Suggestions update as user types
- **Visual rendering**: Shows suggestions below input with highlight on selected item
- **Slash command execution**: Commands starting with "/" are executed locally, not sent to LLM

### 3. Interface Design (`internal/tui/types.go`)
- Created `CommandRegistry` interface to avoid import cycles
- Defined `Command` struct with Name, Aliases, Description, Usage
- Added `Registry` field to `Options` struct
- Added `Execute(ctx, name, args)` method to CommandRegistry interface

### 4. CLI Adapter (`internal/cli/adapter.go`)
- `RegistryAdapter` converts CLI Registry to TUI CommandRegistry interface
- Bridges the gap between CLI and TUI packages without circular dependencies
- Implements `Execute()` method to run slash commands with proper context

### 5. Visual Display
- Suggestions appear below the input field
- Selected suggestion highlighted with "▶" prefix
- Shows command name, usage, and description
- Subtle styling for non-selected items
- Header "Commands:" to indicate suggestion list

## Usage

When the user types "/" in the input field:
1. All available commands are shown
2. As they continue typing (e.g., "/h"), suggestions filter to matching commands
3. Use Up/Down arrows to navigate suggestions
4. Press Tab to accept the highlighted suggestion
5. Press Enter to execute the command (runs locally, not sent to LLM)
6. Press Escape to return to normal mode (clears suggestions)

## Key Fix: Slash Command Execution

**Problem**: Previously, when users typed `/help` in the TUI, it was sent to the LLM as a regular message, resulting in the LLM generating a response instead of executing the built-in command.

**Solution**: Added slash command detection in the Enter key handler:
- Before sending input to the LLM, check if it starts with "/"
- If yes, parse the command name and arguments
- Execute the command via `registry.Execute()`
- Display any errors in the TUI
- Only send to LLM if it's not a slash command

This ensures commands like `/help`, `/history`, `/config`, etc. are executed as built-in commands rather than being interpreted by the LLM.

## Testing

Comprehensive test suite in `internal/tui/suggestions_test.go`:
- ✅ Empty input returns all commands
- ✅ Exact match scoring
- ✅ Prefix match filtering
- ✅ Fuzzy match algorithm
- ✅ No match handling
- ✅ Max results limit
- ✅ Score calculation for various patterns

All tests pass successfully.

## Architecture Benefits

1. **No circular dependencies**: Used interface pattern to decouple TUI from CLI
2. **Testable**: Pure functions for scoring and matching
3. **Extensible**: Easy to add new scoring rules or matching algorithms
4. **Performance**: Simple bubble sort for small result sets (typically < 10 items)
5. **Type-safe**: Strong typing throughout the implementation
6. **Proper command execution**: Slash commands run locally with full context

## Files Modified/Created

### Created:
- `internal/tui/suggestions.go` - Core suggestion logic
- `internal/tui/suggestions_test.go` - Test suite
- `internal/cli/adapter.go` - Registry adapter with Execute support

### Modified:
- `internal/tui/model.go` - Added suggestion state, keyboard handling, and slash command execution
- `internal/tui/types.go` - Added Command struct and CommandRegistry interface with Execute method
- `internal/tui/model_test.go` - Fixed existing test
- `internal/cli/root.go` - Pass registry adapter with slash context to TUI

## Next Steps (Optional Enhancements)

1. Add command history-based ranking (frequently used commands appear first)
2. Implement ghost text completion (show grayed-out completion in input)
3. Add command categories/grouping in suggestions
4. Support mid-input slash detection (not just at start)
5. Add keyboard shortcut to cycle through suggestions (Ctrl+N/Ctrl+P)
6. Show command output in the transcript (currently only shows errors)

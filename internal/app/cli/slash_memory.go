package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/memory"
)

func handleMemoryV2Command(args []string, sc slashContext) error {
	memoryDir := filepath.Join(sc.home, ".claude", "memory")
	config := memory.DefaultSessionMemoryConfig()
	mgr := memory.NewManager(memoryDir, config)

	if len(args) == 0 {
		return showMemoryHelp(sc)
	}

	switch args[0] {
	case "list":
		return handleMemoryList(mgr, args[1:], sc)
	case "show":
		return handleMemoryShow(mgr, args[1:], sc)
	case "search":
		return handleMemorySearch(mgr, args[1:], sc)
	case "filter":
		return handleMemoryFilter(mgr, args[1:], sc)
	case "index":
		return handleMemoryIndex(mgr, sc)
	case "stats":
		return handleMemoryStats(mgr, sc)
	default:
		return fmt.Errorf("unknown memory command: %s", args[0])
	}
}

func showMemoryHelp(sc slashContext) error {
	help := `Memory System Commands:
  /mem list              - List all memories
  /mem show <file>       - Show a specific memory
  /mem search <query>    - Search memories by keyword
  /mem filter <type>     - Filter by type (user/feedback/project/reference)
  /mem index             - Show memory index (MEMORY.md)
  /mem stats             - Show memory statistics`

	_, err := fmt.Fprintln(sc.streams.Out, help)
	return err
}

func handleMemoryList(mgr *memory.Manager, args []string, sc slashContext) error {
	memories, err := mgr.ListMemories()
	if err != nil {
		return fmt.Errorf("failed to list memories: %w", err)
	}

	if len(memories) == 0 {
		_, err = fmt.Fprintln(sc.streams.Out, "No memories found")
		return err
	}

	fmt.Fprintf(sc.streams.Out, "Found %d memories:\n\n", len(memories))
	for _, mem := range memories {
		fmt.Fprintf(sc.streams.Out, "[%s] %s\n", mem.Type, mem.Name)
		fmt.Fprintf(sc.streams.Out, "  File: %s\n", mem.FilePath)
		fmt.Fprintf(sc.streams.Out, "  Description: %s\n", mem.Description)
		fmt.Fprintf(sc.streams.Out, "  Updated: %s\n\n", mem.UpdatedAt.Format("2006-01-02 15:04"))
	}

	return nil
}

func handleMemoryShow(mgr *memory.Manager, args []string, sc slashContext) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /mem show <file>")
	}

	filePath := args[0]
	if !strings.HasSuffix(filePath, ".md") {
		filePath += ".md"
	}

	mem, err := mgr.LoadMemory(filePath)
	if err != nil {
		return fmt.Errorf("failed to load memory: %w", err)
	}

	fmt.Fprintf(sc.streams.Out, "Name: %s\n", mem.Name)
	fmt.Fprintf(sc.streams.Out, "Type: %s\n", mem.Type)
	fmt.Fprintf(sc.streams.Out, "Description: %s\n", mem.Description)
	fmt.Fprintf(sc.streams.Out, "Updated: %s\n\n", mem.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(sc.streams.Out, "---")
	fmt.Fprintln(sc.streams.Out, mem.Content)

	return nil
}

func handleMemorySearch(mgr *memory.Manager, args []string, sc slashContext) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /mem search <query>")
	}

	query := strings.Join(args, " ")
	results, err := mgr.SearchMemories(query)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	if len(results) == 0 {
		_, err = fmt.Fprintf(sc.streams.Out, "No memories found matching '%s'\n", query)
		return err
	}

	fmt.Fprintf(sc.streams.Out, "Found %d memories matching '%s':\n\n", len(results), query)
	for _, mem := range results {
		fmt.Fprintf(sc.streams.Out, "[%s] %s\n", mem.Type, mem.Name)
		fmt.Fprintf(sc.streams.Out, "  %s\n\n", mem.Description)
	}

	return nil
}

func handleMemoryFilter(mgr *memory.Manager, args []string, sc slashContext) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /mem filter <type>\nTypes: user, feedback, project, reference")
	}

	memType := memory.MemoryType(args[0])
	results, err := mgr.FilterByType(memType)
	if err != nil {
		return fmt.Errorf("failed to filter: %w", err)
	}

	if len(results) == 0 {
		_, err = fmt.Fprintf(sc.streams.Out, "No memories of type '%s'\n", memType)
		return err
	}

	fmt.Fprintf(sc.streams.Out, "Found %d memories of type '%s':\n\n", len(results), memType)
	for _, mem := range results {
		fmt.Fprintf(sc.streams.Out, "%s\n", mem.Name)
		fmt.Fprintf(sc.streams.Out, "  %s\n\n", mem.Description)
	}

	return nil
}

func handleMemoryIndex(mgr *memory.Manager, sc slashContext) error {
	index, err := mgr.GetIndex()
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	if len(index.Entries) == 0 {
		_, err = fmt.Fprintln(sc.streams.Out, "Memory index is empty")
		return err
	}

	fmt.Fprintf(sc.streams.Out, "Memory Index (%d entries):\n\n", len(index.Entries))
	for _, entry := range index.Entries {
		fmt.Fprintf(sc.streams.Out, "- [%s](%s)", entry.Title, entry.FilePath)
		if entry.Description != "" {
			fmt.Fprintf(sc.streams.Out, " — %s", entry.Description)
		}
		fmt.Fprintln(sc.streams.Out)
	}

	return nil
}

func handleMemoryStats(mgr *memory.Manager, sc slashContext) error {
	memories, err := mgr.ListMemories()
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	// Count by type
	typeCounts := make(map[memory.MemoryType]int)
	for _, mem := range memories {
		typeCounts[mem.Type]++
	}

	fmt.Fprintln(sc.streams.Out, "Memory Statistics:")
	fmt.Fprintf(sc.streams.Out, "  Total memories: %d\n\n", len(memories))
	fmt.Fprintln(sc.streams.Out, "By type:")
	fmt.Fprintf(sc.streams.Out, "  User:      %d\n", typeCounts[memory.MemoryTypeUser])
	fmt.Fprintf(sc.streams.Out, "  Feedback:  %d\n", typeCounts[memory.MemoryTypeFeedback])
	fmt.Fprintf(sc.streams.Out, "  Project:   %d\n", typeCounts[memory.MemoryTypeProject])
	fmt.Fprintf(sc.streams.Out, "  Reference: %d\n", typeCounts[memory.MemoryTypeReference])

	// Extraction state
	extractor := mgr.GetExtractor()
	state := extractor.GetState()

	fmt.Fprintln(sc.streams.Out, "\nExtraction state:")
	fmt.Fprintf(sc.streams.Out, "  Initialized: %v\n", state.Initialized)
	if state.Initialized {
		fmt.Fprintf(sc.streams.Out, "  Last extraction: %s\n", state.LastExtractionTime.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(sc.streams.Out, "  Token count: %d\n", state.LastExtractionTokenCount)
	}

	return nil
}

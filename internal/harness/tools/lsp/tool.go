package lsp

import (
	"context"
	"encoding/json"
	"fmt"

	lspcore "claude-codex/internal/app/lsp"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type Tool struct {
	rootDir string
	manager *lspcore.LocalManager
}

type input struct {
	Action     string `json:"action"`
	Path       string `json:"path,omitempty"`
	FilePath   string `json:"filePath,omitempty"`
	Query      string `json:"query,omitempty"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
	Character  int    `json:"character,omitempty"`
	MaxResults int    `json:"max_results,omitempty"`
}

func NewTool(args ...any) *Tool {
	rootDir := "."
	var manager *lspcore.LocalManager
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			rootDir = value
		case *lspcore.LocalManager:
			manager = value
		}
	}
	if manager == nil {
		manager = lspcore.NewLocalManager(rootDir)
	}
	return &Tool{rootDir: rootDir, manager: manager}
}

func (t *Tool) Name() string { return "LSP" }
func (t *Tool) Description() string {
	return "LSP operations: workspaceSymbol, documentSymbol, goToDefinition, findReferences, hover"
}
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["workspaceSymbol","documentSymbol","goToDefinition","findReferences","hover","goToImplementation","prepareCallHierarchy","incomingCalls","outgoingCalls","search_symbols","workspace_symbols","document_symbols","go_to_definition","find_references"]},"filePath":{"type":"string","description":"File to operate on"},"path":{"type":"string","description":"Legacy alias for filePath"},"query":{"type":"string"},"line":{"type":"integer"},"character":{"type":"integer","description":"1-based character offset"},"column":{"type":"integer","description":"Legacy alias for character"},"max_results":{"type":"integer"}},"required":["action"]}`)
}
func (t *Tool) Permission() permissions.Level { return permissions.LevelRead }

func (t *Tool) IsConcurrencySafe() bool {
	return true // LSP queries are read-only and safe to run concurrently
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}

	action := normalizeAction(in.Action)
	if in.FilePath == "" {
		in.FilePath = in.Path
	}
	if in.Character == 0 {
		in.Character = in.Column
	}

	switch action {
	case "search_symbols", "workspace_symbols":
		symbols, err := t.manager.SearchSymbols(ctx, t.rootDir, in.Query, in.MaxResults)
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: lspcore.Format(symbols)}, nil

	case "document_symbols":
		if in.FilePath == "" {
			return toolkit.Result{}, fmt.Errorf("path is required for document_symbols")
		}
		path, err := toolkit.ResolvePath(t.rootDir, in.FilePath)
		if err != nil {
			return toolkit.Result{}, err
		}
		symbols, err := t.manager.DocumentSymbols(ctx, path)
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: lspcore.Format(symbols)}, nil

	case "go_to_definition", "definition":
		if in.FilePath == "" {
			return toolkit.Result{}, fmt.Errorf("path is required for go_to_definition")
		}
		if in.Line == 0 || in.Character == 0 {
			return toolkit.Result{}, fmt.Errorf("line and column are required for go_to_definition")
		}
		path, err := toolkit.ResolvePath(t.rootDir, in.FilePath)
		if err != nil {
			return toolkit.Result{}, err
		}
		location, err := t.manager.GoToDefinition(ctx, path, in.Line, in.Character)
		if err != nil {
			return toolkit.Result{}, err
		}
		output := fmt.Sprintf("%s:%d:%d", location.Path, location.Line, location.Col)
		return toolkit.Result{Output: output}, nil

	case "find_references", "references":
		if in.FilePath == "" {
			return toolkit.Result{}, fmt.Errorf("path is required for find_references")
		}
		if in.Line == 0 || in.Character == 0 {
			return toolkit.Result{}, fmt.Errorf("line and column are required for find_references")
		}
		path, err := toolkit.ResolvePath(t.rootDir, in.FilePath)
		if err != nil {
			return toolkit.Result{}, err
		}
		locations, err := t.manager.FindReferences(ctx, path, in.Line, in.Character)
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: lspcore.FormatLocations(locations)}, nil

	case "hover":
		if in.FilePath == "" {
			return toolkit.Result{}, fmt.Errorf("path is required for hover")
		}
		if in.Line == 0 || in.Character == 0 {
			return toolkit.Result{}, fmt.Errorf("line and column are required for hover")
		}
		path, err := toolkit.ResolvePath(t.rootDir, in.FilePath)
		if err != nil {
			return toolkit.Result{}, err
		}
		info, err := t.manager.Hover(ctx, path, in.Line, in.Character)
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: info.Contents}, nil

	default:
		return toolkit.Result{}, fmt.Errorf("unsupported lsp action %q", in.Action)
	}
}

func normalizeAction(action string) string {
	switch action {
	case "workspaceSymbol":
		return "workspace_symbols"
	case "documentSymbol":
		return "document_symbols"
	case "goToDefinition":
		return "go_to_definition"
	case "findReferences":
		return "find_references"
	default:
		return action
	}
}

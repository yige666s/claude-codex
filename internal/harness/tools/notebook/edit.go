package notebook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
	"claude-codex/internal/public/fsutil"
)

type EditTool struct {
	rootDir string
}

type input struct {
	Path      string         `json:"path"`
	Operation string         `json:"operation"`
	Index     int            `json:"index,omitempty"`
	CellType  string         `json:"cell_type,omitempty"`
	Source    string         `json:"source,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func NewEditTool(rootDir string) *EditTool {
	return &EditTool{rootDir: rootDir}
}

func (t *EditTool) Name() string {
	return "NotebookEdit"
}

func (t *EditTool) Description() string {
	return "Append, replace, or delete cells in a Jupyter notebook."
}

func (t *EditTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"operation":{"type":"string"},"index":{"type":"integer"},"cell_type":{"type":"string"},"source":{"type":"string"}},"required":["path","operation"]}`)
}

func (t *EditTool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *EditTool) IsConcurrencySafe() bool {
	return false // notebook edits are not safe to run concurrently
}

func (t *EditTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	path, err := toolkit.ResolvePath(t.rootDir, in.Path)
	if err != nil {
		return toolkit.Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolkit.Result{}, err
	}

	var notebook map[string]any
	if err := json.Unmarshal(data, &notebook); err != nil {
		return toolkit.Result{}, err
	}

	cells, err := notebookCells(notebook)
	if err != nil {
		return toolkit.Result{}, err
	}

	switch in.Operation {
	case "append":
		cells = append(cells, newCell(in))
	case "replace":
		if in.Index < 0 || in.Index >= len(cells) {
			return toolkit.Result{}, fmt.Errorf("replace index %d out of range", in.Index)
		}
		cells[in.Index] = replaceCell(cells[in.Index], in)
	case "delete":
		if in.Index < 0 || in.Index >= len(cells) {
			return toolkit.Result{}, fmt.Errorf("delete index %d out of range", in.Index)
		}
		cells = append(cells[:in.Index], cells[in.Index+1:]...)
	default:
		return toolkit.Result{}, fmt.Errorf("unsupported operation %q", in.Operation)
	}
	notebook["cells"] = cells

	updated, err := json.MarshalIndent(notebook, "", "  ")
	if err != nil {
		return toolkit.Result{}, err
	}
	if err := fsutil.WriteFileAtomic(path, updated, 0o644); err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: fmt.Sprintf("updated notebook %s", path)}, nil
}

func newCell(in input) map[string]any {
	cellType := in.CellType
	if cellType == "" {
		cellType = "markdown"
	}
	cell := map[string]any{
		"cell_type": cellType,
		"metadata":  map[string]any{},
		"source":    splitNotebookSource(in.Source),
	}
	if in.Metadata != nil {
		cell["metadata"] = in.Metadata
	}
	if cellType == "code" {
		cell["outputs"] = []any{}
		cell["execution_count"] = nil
	}
	return cell
}

func splitNotebookSource(value string) []string {
	if value == "" {
		return []string{}
	}
	lines := strings.SplitAfter(value, "\n")
	if len(lines) == 0 {
		return []string{value}
	}
	return lines
}

func notebookCells(document map[string]any) ([]map[string]any, error) {
	raw, ok := document["cells"]
	if !ok {
		return []map[string]any{}, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("notebook cells field is not an array")
	}

	cells := make([]map[string]any, 0, len(list))
	for _, item := range list {
		cell, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("notebook cell has unexpected shape")
		}
		cells = append(cells, cell)
	}
	return cells, nil
}

func replaceCell(existing map[string]any, in input) map[string]any {
	cell := cloneMap(existing)
	if in.CellType != "" {
		cell["cell_type"] = in.CellType
	}
	if in.Source != "" {
		cell["source"] = splitNotebookSource(in.Source)
	}
	if in.Metadata != nil {
		cell["metadata"] = in.Metadata
	}
	if cellType, _ := cell["cell_type"].(string); cellType == "code" {
		if _, ok := cell["outputs"]; !ok {
			cell["outputs"] = []any{}
		}
		if _, ok := cell["execution_count"]; !ok {
			cell["execution_count"] = nil
		}
	}
	return cell
}

func cloneMap(value map[string]any) map[string]any {
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

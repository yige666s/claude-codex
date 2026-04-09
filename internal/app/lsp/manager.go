package lsp

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Symbol struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type Location struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type HoverInfo struct {
	Contents string   `json:"contents"`
	Location Location `json:"location"`
}

type LocalManager struct{}

func NewLocalManager(_ ...string) *LocalManager {
	return &LocalManager{}
}

func (m *LocalManager) SearchSymbols(ctx context.Context, root, query string, maxResults int) ([]Symbol, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	results := make([]Symbol, 0, maxResults)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		symbols, err := m.DocumentSymbols(ctx, path)
		if err != nil {
			return nil
		}
		for _, sym := range symbols {
			if strings.Contains(strings.ToLower(sym.Name), strings.ToLower(query)) {
				results = append(results, sym)
				if len(results) >= maxResults {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return nil, err
	}
	return results, nil
}

func (m *LocalManager) DocumentSymbols(ctx context.Context, path string) ([]Symbol, error) {
	// Try Go AST parsing first for .go files
	if strings.HasSuffix(path, ".go") {
		symbols, err := m.parseGoFile(ctx, path)
		if err == nil {
			return symbols, nil
		}
		// Fall back to regex if AST parsing fails
	}

	// Regex-based parsing for other files
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	patterns := []struct {
		kind string
		re   *regexp.Regexp
	}{
		{"func", regexp.MustCompile(`^\s*func\s+([A-Za-z0-9_]+)`)},
		{"type", regexp.MustCompile(`^\s*type\s+([A-Za-z0-9_]+)`)},
		{"var", regexp.MustCompile(`^\s*var\s+([A-Za-z0-9_]+)`)},
		{"const", regexp.MustCompile(`^\s*const\s+([A-Za-z0-9_]+)`)},
		{"class", regexp.MustCompile(`^\s*class\s+([A-Za-z0-9_]+)`)},
		{"interface", regexp.MustCompile(`^\s*interface\s+([A-Za-z0-9_]+)`)},
	}

	relative := filepath.ToSlash(path)
	var symbols []Symbol
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lineNo++
		line := scanner.Text()
		for _, pattern := range patterns {
			match := pattern.re.FindStringSubmatch(line)
			if len(match) == 2 {
				symbols = append(symbols, Symbol{
					Path: relative,
					Line: lineNo,
					Name: match[1],
					Kind: pattern.kind,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return symbols, nil
}

// parseGoFile uses Go's AST parser for accurate symbol extraction
func (m *LocalManager) parseGoFile(ctx context.Context, path string) ([]Symbol, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol
	relative := filepath.ToSlash(path)

	ast.Inspect(node, func(n ast.Node) bool {
		if ctx.Err() != nil {
			return false
		}

		switch decl := n.(type) {
		case *ast.FuncDecl:
			pos := fset.Position(decl.Name.Pos())
			symbols = append(symbols, Symbol{
				Path: relative,
				Line: pos.Line,
				Name: decl.Name.Name,
				Kind: "func",
			})
		case *ast.TypeSpec:
			pos := fset.Position(decl.Name.Pos())
			kind := "type"
			if _, ok := decl.Type.(*ast.InterfaceType); ok {
				kind = "interface"
			} else if _, ok := decl.Type.(*ast.StructType); ok {
				kind = "struct"
			}
			symbols = append(symbols, Symbol{
				Path: relative,
				Line: pos.Line,
				Name: decl.Name.Name,
				Kind: kind,
			})
		case *ast.ValueSpec:
			for _, name := range decl.Names {
				pos := fset.Position(name.Pos())
				symbols = append(symbols, Symbol{
					Path: relative,
					Line: pos.Line,
					Name: name.Name,
					Kind: "var",
				})
			}
		}
		return true
	})

	return symbols, nil
}

// GoToDefinition finds the definition of a symbol at the given position
func (m *LocalManager) GoToDefinition(ctx context.Context, path string, line, col int) (*Location, error) {
	// Read the file to get the symbol at the position
	symbolName, err := m.getSymbolAtPosition(path, line, col)
	if err != nil {
		return nil, err
	}

	// Search for the definition in the workspace
	root := filepath.Dir(path)
	for i := 0; i < 3; i++ { // Go up 3 levels to find project root
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}

	var result *Location
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		symbols, err := m.DocumentSymbols(ctx, p)
		if err != nil {
			return nil
		}

		for _, sym := range symbols {
			if sym.Name == symbolName {
				result = &Location{
					Path: sym.Path,
					Line: sym.Line,
					Col:  1,
				}
				return filepath.SkipAll
			}
		}
		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("definition not found for %s", symbolName)
	}
	return result, nil
}

// FindReferences finds all references to a symbol
func (m *LocalManager) FindReferences(ctx context.Context, path string, line, col int) ([]Location, error) {
	symbolName, err := m.getSymbolAtPosition(path, line, col)
	if err != nil {
		return nil, err
	}

	root := filepath.Dir(path)
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}

	var locations []Location
	symbolRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(symbolName) + `\b`)

	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		file, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if symbolRe.MatchString(line) {
				col := strings.Index(line, symbolName) + 1
				locations = append(locations, Location{
					Path: filepath.ToSlash(p),
					Line: lineNo,
					Col:  col,
				})
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return locations, nil
}

// Hover provides information about a symbol at the given position
func (m *LocalManager) Hover(ctx context.Context, path string, line, col int) (*HoverInfo, error) {
	symbolName, err := m.getSymbolAtPosition(path, line, col)
	if err != nil {
		return nil, err
	}

	// Find the definition
	def, err := m.GoToDefinition(ctx, path, line, col)
	if err != nil {
		return &HoverInfo{
			Contents: fmt.Sprintf("Symbol: %s (definition not found)", symbolName),
			Location: Location{Path: path, Line: line, Col: col},
		}, nil
	}

	// Read the definition line for context
	defLine, err := m.getLineContent(def.Path, def.Line)
	if err != nil {
		defLine = ""
	}

	return &HoverInfo{
		Contents: fmt.Sprintf("Symbol: %s\nDefined at: %s:%d\n%s", symbolName, def.Path, def.Line, defLine),
		Location: *def,
	}, nil
}

// getSymbolAtPosition extracts the symbol name at the given position
func (m *LocalManager) getSymbolAtPosition(path string, line, col int) (string, error) {
	content, err := m.getLineContent(path, line)
	if err != nil {
		return "", err
	}

	if col > len(content) {
		col = len(content)
	}

	// Find word boundaries
	start := col - 1
	for start > 0 && isIdentChar(content[start-1]) {
		start--
	}
	end := col - 1
	for end < len(content) && isIdentChar(content[end]) {
		end++
	}

	if start >= end {
		return "", fmt.Errorf("no symbol at position")
	}

	return content[start:end], nil
}

// getLineContent reads a specific line from a file
func (m *LocalManager) getLineContent(path string, line int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine == line {
			return scanner.Text(), nil
		}
	}
	return "", fmt.Errorf("line %d not found", line)
}

// isIdentChar checks if a character is valid in an identifier
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func Format(symbols []Symbol) string {
	lines := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		lines = append(lines, fmt.Sprintf("%s:%d [%s] %s", symbol.Path, symbol.Line, symbol.Kind, symbol.Name))
	}
	return strings.Join(lines, "\n")
}

func FormatLocations(locations []Location) string {
	lines := make([]string, 0, len(locations))
	for _, loc := range locations {
		lines = append(lines, fmt.Sprintf("%s:%d:%d", loc.Path, loc.Line, loc.Col))
	}
	return strings.Join(lines, "\n")
}

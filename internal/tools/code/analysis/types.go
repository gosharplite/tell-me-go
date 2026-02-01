package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type TypeManager struct {
	Indexer index.SymbolIndex
	Cache   *astutil.ASTCache
	SP      types.SecurityProvider
}

func NewTypeManager(idx index.SymbolIndex, cache *astutil.ASTCache, sp types.SecurityProvider) *TypeManager {
	return &TypeManager{
		Indexer: idx,
		Cache:   cache,
		SP:      sp,
	}
}

func (m *TypeManager) GetTypeInfo(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Typename string `json:"typename"`
		Path     string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	typename := params.Typename
	if typename == "" {
		return types.ToolResult{Text: "Please provide a typename."}, nil
	}

	if err := m.Indexer.Refresh(ctx); err != nil {
		return types.ToolResult{}, err
	}

	locs, err := m.Indexer.Lookup(ctx, typename)
	if err != nil || len(locs) == 0 {
		return types.ToolResult{Text: "Type not found."}, nil
	}

	// For now, take the first definition
	loc := locs[0]
	
	f, _, err := m.Cache.Get(loc.Path)
	if err != nil {
		return types.ToolResult{}, err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Type: %s\nLocation: %s:%d\n", typename, loc.Path, loc.Line))

	found := false
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				if ts.Name.Name == typename {
					found = true
					if gd.Doc != nil {
						sb.WriteString("Doc: " + gd.Doc.Text())
					}
					switch t := ts.Type.(type) {
					case *ast.StructType:
						sb.WriteString("Fields:\n")
						for _, field := range t.Fields.List {
							names := []string{}
							for _, n := range field.Names {
								names = append(names, n.Name)
							}
							tag := ""
							if field.Tag != nil {
								tag = " " + field.Tag.Value
							}
							sb.WriteString(fmt.Sprintf("  - %s %s%s\n", strings.Join(names, ", "), astutil.ExprToString(field.Type), tag))
						}
					case *ast.InterfaceType:
						sb.WriteString("Methods:\n")
						for _, m := range t.Methods.List {
							if len(m.Names) > 0 {
								sb.WriteString(fmt.Sprintf("  - %s\n", m.Names[0].Name))
							}
						}
					}
					break
				}
			}
		}
		if found {
			break
		}
	}

	// Find methods (receivers) in the same package
	// Optimized: instead of walking the whole project, we only walk the directory of the found type
	// (Go requires methods to be in the same package, and usually same directory)
	dir := filepath.Dir(loc.Path)
	sb.WriteString("Methods (Receivers):\n")
	err = filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || filepath.Ext(p) != ".go" {
			return nil
		}
		ff, _, err := m.Cache.Get(p)
		if err != nil {
			return nil
		}
		for _, d := range ff.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
				recvType := astutil.ExprToString(fd.Recv.List[0].Type)
				if strings.TrimPrefix(recvType, "*") == typename {
					sb.WriteString(fmt.Sprintf("  - %s\n", astutil.GetFuncSignature(fd)))
				}
			}
		}
		return nil
	})
	if err != nil {
		return types.ToolResult{}, err
	}

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *TypeManager) ListSymbols(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path         string `json:"path"`
		ExportedOnly bool   `json:"exported_only"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}
	resolvedPath, err := m.SP.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string
	err = filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, fset, err := m.Cache.Get(filePath)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if params.ExportedOnly && !ast.IsExported(d.Name.Name) {
					continue
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, fset.Position(d.Pos()).Line, astutil.GetFuncSignature(d)))
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if params.ExportedOnly && !ast.IsExported(s.Name.Name) {
							continue
						}
						results = append(results, fmt.Sprintf("%s:%d: type %s", filePath, fset.Position(s.Pos()).Line, s.Name.Name))
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if params.ExportedOnly && !ast.IsExported(name.Name) {
								continue
							}
							results = append(results, fmt.Sprintf("%s:%d: %s %s", filePath, fset.Position(name.Pos()).Line, d.Tok, name.Name))
						}
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No symbols found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *TypeManager) ListImplementations(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		InterfaceName string `json:"interface_name"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	if params.InterfaceName == "" {
		return types.ToolResult{Text: "Please provide an interface_name."}, nil
	}

	if err := m.Indexer.Refresh(ctx); err != nil {
		return types.ToolResult{}, err
	}

	implementors, err := m.Indexer.FindImplementors(ctx, params.InterfaceName)
	if err != nil {
		return types.ToolResult{}, err
	}

	if len(implementors) == 0 {
		return types.ToolResult{Text: fmt.Sprintf("No implementors found for %s", params.InterfaceName)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Types implementing %s:\n", params.InterfaceName))
	for _, imp := range implementors {
		sb.WriteString(fmt.Sprintf("- %s.%s\n", imp.PkgPath, imp.Name))
	}

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *TypeManager) FindUsages(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	query := params.Query
	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.SP.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string

	err = filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		f, fset, err := m.Cache.Get(filePath)
		if err != nil {
			return nil
		}

		// Read file lines for context
		var lines []string
		if content, err := os.ReadFile(filePath); err == nil {
			lines = strings.Split(string(content), "\n")
		}

		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if id.Name == query {
					pos := fset.Position(id.Pos())
					lineContent := ""
					if pos.Line > 0 && pos.Line <= len(lines) {
						lineContent = strings.TrimSpace(lines[pos.Line-1])
					}
					results = append(results, fmt.Sprintf("%s:%d:%d: %s", filePath, pos.Line, pos.Column, lineContent))
				}
			}
			return true
		})
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No usages found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *TypeManager) FindDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}
	resolvedPath, err := m.SP.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	results, err := m.GrepDefinitionsGo(ctx, resolvedPath, params.Query)
	if err != nil {
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No definitions found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *TypeManager) GrepDefinitionsGo(ctx context.Context, path, query string) ([]string, error) {
	var results []string
	var parseErrors []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, fset, err := m.Cache.Get(filePath)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", filePath, err))
			return nil
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				name := d.Name.Name
				if query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
					line := fset.Position(d.Pos()).Line
					sig := astutil.GetFuncSignature(d)
					results = append(results, fmt.Sprintf("%s:%d: %s", filePath, line, sig))
				}
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					for _, spec := range d.Specs {
						tSpec := spec.(*ast.TypeSpec)
						name := tSpec.Name.Name
						if query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
							line := fset.Position(tSpec.Pos()).Line
							results = append(results, fmt.Sprintf("%s:%d: type %s", filePath, line, name))
						}
					}
				}
			}
		}
		return nil
	})

	if len(results) == 0 && len(parseErrors) > 0 {
		return nil, fmt.Errorf("failed to parse Go files:\n%s", strings.Join(parseErrors, "\n"))
	}

	return results, err
}

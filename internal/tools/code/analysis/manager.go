package analysis

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type CommandExecutor interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

type RealExecutor struct{}

func (e *RealExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (e *RealExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type AnalysisManager struct {
	Indexer index.SymbolIndex
	Cache   *astutil.ASTCache
	SP      types.SecurityProvider
	Exec    CommandExecutor
}

func NewAnalysisManager(idx index.SymbolIndex, cache *astutil.ASTCache, sp types.SecurityProvider) *AnalysisManager {
	return &AnalysisManager{
		Indexer: idx,
		Cache:   cache,
		SP:      sp,
		Exec:    &RealExecutor{},
	}
}

func (m *AnalysisManager) AnalyzeComplexity(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
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
			if fd, ok := decl.(*ast.FuncDecl); ok {
				complexity := 1
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch t := n.(type) {
					case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause, *ast.CommClause:
						complexity++
					case *ast.BinaryExpr:
						if t.Op == token.LAND || t.Op == token.LOR {
							complexity++
						}
					}
					return true
				})
				funcName := fd.Name.Name
				if fd.Recv != nil {
					recvType := astutil.ExprToString(fd.Recv.List[0].Type)
					funcName = fmt.Sprintf("(%s).%s", recvType, funcName)
				}
				results = append(results, fmt.Sprintf("%s:%d: %s - Complexity: %d", filePath, fset.Position(fd.Pos()).Line, funcName, complexity))
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No Go functions found to analyze."}, nil
	}

	// Sort by complexity descending
	sort.Slice(results, func(i, j int) bool {
		var ci, cj int
		fmt.Sscanf(results[i], "%*[^:]: %*d: %*s - Complexity: %d", &ci)
		fmt.Sscanf(results[j], "%*[^:]: %*d: %*s - Complexity: %d", &cj)
		return ci > cj
	})

	if len(results) > 100 {
		results = append(results[:100], "... (truncated)")
	}

	return types.ToolResult{Text: "Cyclomatic Complexity Analysis (Top 100):\n" + strings.Join(results, "\n")}, nil
}

func (m *AnalysisManager) GetPackageGraph(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	func() {
		m.SP.TerminalLock()
		defer m.SP.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Analyzing package dependencies\033[0m\n")
	}()

	out, err := m.Exec.CombinedOutput(ctx, "go", "list", "-f", "{{.ImportPath}} -> {{.Imports}}", "./...")
	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error listing packages: %v\nOutput: %s", err, string(out))}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var sb strings.Builder
	sb.WriteString("Internal Package Dependency Graph:\n")

	// Get module name to filter for internal imports
	modOut, _ := m.Exec.Output(ctx, "go", "list", "-m")
	modName := strings.TrimSpace(string(modOut))

	for _, line := range lines {
		parts := strings.Split(line, " -> ")
		if len(parts) != 2 {
			continue
		}
		pkg := parts[0]
		importsRaw := strings.Trim(parts[1], "[]")
		imports := strings.Fields(importsRaw)

		var internalImports []string
		for _, imp := range imports {
			if strings.HasPrefix(imp, modName) {
				internalImports = append(internalImports, imp)
			}
		}

		if len(internalImports) > 0 {
			sb.WriteString(fmt.Sprintf("%s\n", pkg))
			for _, imp := range internalImports {
				sb.WriteString(fmt.Sprintf("  └── %s\n", imp))
			}
		} else {
			sb.WriteString(fmt.Sprintf("%s (no internal dependencies)\n", pkg))
		}
	}

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *AnalysisManager) SemanticDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Target string `json:"target"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	target := params.Target

	// 1. Get stats and summary as before
	statOut, _ := m.Exec.CombinedOutput(ctx, "git", "diff", "--stat", target)
	summaryOut, _ := m.Exec.CombinedOutput(ctx, "git", "diff", "--summary", target)

	var sb strings.Builder
	sb.WriteString("Semantic Diff Summary:\n\n")
	sb.WriteString("File Statistics:\n")
	sb.WriteString(string(statOut))
	sb.WriteString("\nChange Summary:\n")
	sb.WriteString(string(summaryOut))

	// 2. Logical Analysis
	sb.WriteString("\nLogical Code Changes:\n")

	// Get list of changed .go files
	filesOut, err := m.Exec.CombinedOutput(ctx, "git", "diff", "--name-only", target)
	if err != nil {
		return types.ToolResult{Text: sb.String() + "\n(Could not perform logical analysis)"}, nil
	}

	changedFiles := strings.Split(strings.TrimSpace(string(filesOut)), "\n")
	fset := token.NewFileSet()

	for _, relPath := range changedFiles {
		if filepath.Ext(relPath) != ".go" || strings.Contains(relPath, "vendor/") {
			continue
		}

		// Get current AST
		currAST, _, err := m.Cache.Get(relPath)
		if err != nil {
			continue // Skip unparsable current files
		}

		// Get target AST (base)
		var baseAST *ast.File
		baseContent, err := m.Exec.Output(ctx, "git", "show", target+":"+relPath)
		if err == nil {
			baseAST, _ = parser.ParseFile(fset, relPath, baseContent, parser.ParseComments)
		}

		var changes []string
		if baseAST == nil {
			// Entirely new file
			for _, d := range currAST.Decls {
				key := GetDeclKey(d)
				if key != "unknown" {
					changes = append(changes, "Added: "+key)
				}
			}
		} else {
			changes = CompareASTs(baseAST, currAST)
		}
		if len(changes) > 0 {
			sb.WriteString(fmt.Sprintf("\n[%s]\n", relPath))
			for _, ch := range changes {
				sb.WriteString(fmt.Sprintf("  - %s\n", ch))
			}
		}
	}

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *AnalysisManager) ListImplementations(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		InterfaceName string `json:"interface_name"` // Adjusted from path to be more useful
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

func CompareASTs(base, curr *ast.File) []string {
	var changes []string

	baseDecls := map[string]ast.Decl{}
	for _, d := range base.Decls {
		baseDecls[GetDeclKey(d)] = d
	}

	currDecls := map[string]ast.Decl{}
	for _, d := range curr.Decls {
		currDecls[GetDeclKey(d)] = d
	}

	// Find Added and Modified
	var keys []string
	for k := range currDecls {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		currDecl := currDecls[k]
		if baseDecl, ok := baseDecls[k]; !ok {
			changes = append(changes, "Added: "+k)
		} else {
			if !IsDeclEqual(baseDecl, currDecl) {
				changes = append(changes, "Modified: "+k)
			}
		}
	}

	// Find Deleted
	var baseKeys []string
	for k := range baseDecls {
		baseKeys = append(baseKeys, k)
	}
	sort.Strings(baseKeys)

	for _, k := range baseKeys {
		if _, ok := currDecls[k]; !ok {
			changes = append(changes, "Deleted: "+k)
		}
	}

	return changes
}

func GetDeclKey(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		name := d.Name.Name
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := astutil.ExprToString(d.Recv.List[0].Type)
			return fmt.Sprintf("func (%s) %s", recv, name)
		}
		return "func " + name
	case *ast.GenDecl:
		if d.Tok == token.TYPE && len(d.Specs) > 0 {
			if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
				return "type " + ts.Name.Name
			}
		}
		if d.Tok == token.CONST && len(d.Specs) > 0 {
			return "const block"
		}
		if d.Tok == token.VAR && len(d.Specs) > 0 {
			return "var block"
		}
	}
	return "unknown"
}

func IsDeclEqual(a, b ast.Decl) bool {
	// Crude but effective for semantic diff: compare formatted strings
	fset := token.NewFileSet()
	var bufA, bufB bytes.Buffer
	if err := format.Node(&bufA, fset, a); err != nil {
		return false
	}
	if err := format.Node(&bufB, fset, b); err != nil {
		return false
	}
	return bufA.String() == bufB.String()
}

func (m *AnalysisManager) FindUsages(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

func (m *AnalysisManager) ListSymbols(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

func (m *AnalysisManager) GetTypeInfo(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Typename string `json:"typename"`
		Path     string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	typename := params.Typename
	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.SP.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	var sb strings.Builder

	err = filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, _, err := m.Cache.Get(filePath)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					if ts.Name.Name == typename {
						sb.WriteString(fmt.Sprintf("Type: %s\nLocation: %s\n", typename, filePath))
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

						// Find methods
						sb.WriteString("Methods (Receivers):\n")
						filepath.Walk(resolvedPath, func(p string, i os.FileInfo, e error) error {
							select {
							case <-ctx.Done():
								return ctx.Err()
							default:
							}
							if e != nil || i.IsDir() || filepath.Ext(p) != ".go" {
								return nil
							}
							ff, _, _ := m.Cache.Get(p)
							if ff == nil {
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
						return filepath.SkipDir
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}
	if sb.Len() == 0 {
		return types.ToolResult{Text: "Type not found."}, nil
	}
	return types.ToolResult{Text: sb.String()}, nil
}

func (m *AnalysisManager) FindDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

func (m *AnalysisManager) GrepDefinitionsGo(ctx context.Context, path, query string) ([]string, error) {
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
			return nil // Skip files with syntax errors but track them
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

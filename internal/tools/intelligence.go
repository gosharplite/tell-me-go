// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/genai"
)

// RegisterIntelligenceTools adds AST-based tools to the registry.
func RegisterIntelligenceTools(r *Registry) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "find_usages",
		Description: "Identify all references to a specific symbol across the project.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"query": {
					Type:        genai.TypeString,
					Description: "The symbol name to find.",
				},
				"path": {
					Type:        genai.TypeString,
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"query"},
		},
	}, findUsages)

	r.Register(&genai.FunctionDeclaration{
		Name:        "list_implementations",
		Description: "Map the relationship between interfaces and structs in the codebase.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory to search (defaults to '.')",
				},
			},
		},
	}, listImplementations)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_type_info",
		Description: "Provide a deep dive into a specific type (fields, methods, implementations).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"typename": {
					Type:        genai.TypeString,
					Description: "The name of the type to inspect.",
				},
				"path": {
					Type:        genai.TypeString,
					Description: "The directory to search for the type (defaults to '.')",
				},
			},
			Required: []string{"typename"},
		},
	}, getTypeInfo)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_project_summary",
		Description: "Returns a high-level summary of the project architecture, including packages, file counts, and Go module info.",
	}, getProjectSummary)

	r.Register(&genai.FunctionDeclaration{
		Name:        "search_usages_globally",
		Description: "Searches for a string or symbol usage across all file types in the project, with smart exclusions.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"query": {
					Type:        genai.TypeString,
					Description: "The string or regex to search for.",
				},
			},
			Required: []string{"query"},
		},
	}, searchUsagesGlobally)

	r.Register(&genai.FunctionDeclaration{
		Name:        "semantic_diff",
		Description: "Provides a summarized, logical view of changes between the current state and a commit/branch, focusing on function and structural changes.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"target": {
					Type:        genai.TypeString,
					Description: "The git target (commit hash, branch name, or 'HEAD~1') to compare against.",
				},
			},
			Required: []string{"target"},
		},
	}, semanticDiff)

	r.Register(&genai.FunctionDeclaration{
		Name:        "rename_symbol",
		Description: "Safely renames a Go symbol (function, type, variable) across the project using AST.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"old_name": {
					Type:        genai.TypeString,
					Description: "The current name of the symbol.",
				},
				"new_name": {
					Type:        genai.TypeString,
					Description: "The new name for the symbol.",
				},
				"path": {
					Type:        genai.TypeString,
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"old_name", "new_name"},
		},
	}, renameSymbol)

	r.Register(&genai.FunctionDeclaration{
		Name:        "list_todos",
		Description: "Scans the project for TODO, FIXME, or BUG comments.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory to scan (defaults to '.')",
				},
			},
		},
	}, listTodos)

	r.Register(&genai.FunctionDeclaration{
		Name:        "go_doc",
		Description: "Runs 'go doc' to retrieve documentation for a package or symbol.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"symbol": {
					Type:        genai.TypeString,
					Description: "The package or symbol to get documentation for (e.g., 'fmt.Println', './internal/tools').",
				},
			},
			Required: []string{"symbol"},
		},
	}, goDoc)

	r.Register(&genai.FunctionDeclaration{
		Name:        "analyze_complexity",
		Description: "Calculates the cyclomatic complexity of Go functions in a file or directory.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The file or directory to analyze.",
				},
			},
			Required: []string{"path"},
		},
	}, analyzeComplexity)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_package_graph",
		Description: "Returns a mapping of internal package dependencies to help understand project architecture.",
	}, getPackageGraph)
}

// AST-based helpers for existing tools

func grepDefinitionsGo(path, query string) ([]string, error) {
	var results []string
	fset := token.NewFileSet()

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil // Skip files with syntax errors
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				name := d.Name.Name
				if query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
					line := fset.Position(d.Pos()).Line
					sig := getFuncSignature(d)
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

	return results, err
}

func getFileSkeletonGo(filePath string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("failed to parse Go file: %w", err)
	}

	var sb strings.Builder
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				sb.WriteString(d.Doc.Text())
			}
			sb.WriteString(getFuncSignature(d) + "\n\n")
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				if d.Doc != nil {
					sb.WriteString(d.Doc.Text())
				}
				for _, spec := range d.Specs {
					tSpec := spec.(*ast.TypeSpec)
					sb.WriteString(fmt.Sprintf("type %s ", tSpec.Name.Name))
					switch t := tSpec.Type.(type) {
					case *ast.StructType:
						sb.WriteString("struct { ... }\n")
					case *ast.InterfaceType:
						sb.WriteString("interface { ... }\n")
					default:
						_ = t
						sb.WriteString("...\n")
					}
				}
				sb.WriteString("\n")
			}
		}
	}

	return sb.String(), nil
}

func renameSymbol(args map[string]interface{}) (string, error) {
	oldName, _ := args["old_name"].(string)
	newName, _ := args["new_name"].(string)
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	totalFiles := 0
	totalChanges := 0

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		changed := false
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == oldName {
				id.Name = newName
				changed = true
				totalChanges++
			}
			return true
		})

		if changed {
			totalFiles++
			// Write back formatted
			var buf bytes.Buffer
			if err := format.Node(&buf, fset, f); err != nil {
				return fmt.Errorf("failed to format %s: %w", filePath, err)
			}
			if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", filePath, err)
			}
		}
		return nil
	})

	if totalChanges == 0 {
		return fmt.Sprintf("Symbol '%s' not found.", oldName), nil
	}

	return fmt.Sprintf("Renamed %d occurrences of '%s' to '%s' in %d files.", totalChanges, oldName, newName, totalFiles), err
}

func listTodos(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	re := regexp.MustCompile(`(?i)(TODO|FIXME|BUG):?.*`)
	var results []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				match := re.FindString(line)
				trimmed := strings.TrimSpace(match)
				if len(trimmed) > 500 {
					trimmed = trimmed[:500] + " (truncated)"
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, i+1, trimmed))
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No TODOs, FIXMEs, or BUGs found.", nil
	}
	return strings.Join(results, "\n"), nil
}

func goDoc(args map[string]interface{}) (string, error) {
	symbol, _ := args["symbol"].(string)
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running go doc %s\033[0m\n", symbol)

	cmd := exec.Command("go", "doc", symbol)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error running go doc: %v\nOutput: %s", err, string(out)), nil
	}

	return string(out), nil
}

func analyzeComplexity(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if err := IsPathSafe(path); err != nil {
		return "", err
	}

	var results []string
	fset := token.NewFileSet()

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, err := parser.ParseFile(fset, filePath, nil, 0)
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
					recvType := exprToString(fd.Recv.List[0].Type)
					funcName = fmt.Sprintf("(%s).%s", recvType, funcName)
				}
				results = append(results, fmt.Sprintf("%s:%d: %s - Complexity: %d", filePath, fset.Position(fd.Pos()).Line, funcName, complexity))
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No Go functions found to analyze.", nil
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

	return "Cyclomatic Complexity Analysis (Top 100):\n" + strings.Join(results, "\n"), nil
}

func getPackageGraph(args map[string]interface{}) (string, error) {
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Analyzing package dependencies\033[0m\n")

	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} -> {{.Imports}}", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Error listing packages: %v\nOutput: %s", err, string(out)), nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var sb strings.Builder
	sb.WriteString("Internal Package Dependency Graph:\n")

	// Get module name to filter for internal imports
	modCmd := exec.Command("go", "list", "-m")
	modOut, _ := modCmd.Output()
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

	return sb.String(), nil
}

func getFuncSignature(f *ast.FuncDecl) string {
	var sb strings.Builder
	sb.WriteString("func ")
	if f.Recv != nil {
		sb.WriteString("(")
		for i, field := range f.Recv.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			if len(field.Names) > 0 {
				sb.WriteString(field.Names[0].Name + " ")
			}
			sb.WriteString(exprToString(field.Type))
		}
		sb.WriteString(") ")
	}
	sb.WriteString(f.Name.Name + "(")
	if f.Type.Params != nil {
		for i, field := range f.Type.Params.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			for j, name := range field.Names {
				if j > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(name.Name)
			}
			sb.WriteString(" " + exprToString(field.Type))
		}
	}
	sb.WriteString(")")
	if f.Type.Results != nil {
		sb.WriteString(" ")
		if len(f.Type.Results.List) > 1 {
			sb.WriteString("(")
		}
		for i, field := range f.Type.Results.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(exprToString(field.Type))
		}
		if len(f.Type.Results.List) > 1 {
			sb.WriteString(")")
		}
	}
	return sb.String()
}

func exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	case *ast.ArrayType:
		return "[]" + exprToString(t.Elt)
	case *ast.MapType:
		return "map[" + exprToString(t.Key) + "]" + exprToString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprToString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	default:
		return fmt.Sprintf("%T", t)
	}
}

// New Intelligence Tools Implementation

func findUsages(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	fset := token.NewFileSet()
	var results []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if id.Name == query {
					pos := fset.Position(id.Pos())
					results = append(results, fmt.Sprintf("%s:%d:%d", filePath, pos.Line, pos.Column))
				}
			}
			return true
		})
		return nil
	})

	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "No usages found.", nil
	}
	return strings.Join(results, "\n"), nil
}

func listImplementations(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	fset := token.NewFileSet()
	type interfaceInfo struct {
		methods []string
		path    string
	}
	interfaces := make(map[string]interfaceInfo)

	type structInfo struct {
		methods []string
		path    string
	}
	structs := make(map[string]structInfo)

	// Phase 1: Collect all interfaces and structs
	filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					if it, ok := ts.Type.(*ast.InterfaceType); ok {
						var methods []string
						if it.Methods != nil {
							for _, m := range it.Methods.List {
								if len(m.Names) > 0 {
									methods = append(methods, m.Names[0].Name)
								}
							}
						}
						interfaces[ts.Name.Name] = interfaceInfo{methods: methods, path: filePath}
					} else if _, ok := ts.Type.(*ast.StructType); ok {
						structs[ts.Name.Name] = structInfo{path: filePath}
					}
				}
			}
		}
		return nil
	})

	// Phase 2: Collect all methods for structs
	filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			return nil
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil {
				recvType := exprToString(fd.Recv.List[0].Type)
				recvType = strings.TrimPrefix(recvType, "*")
				if info, ok := structs[recvType]; ok {
					info.methods = append(info.methods, fd.Name.Name)
					structs[recvType] = info
				}
			}
		}
		return nil
	})

	// Phase 3: Match
	var sb strings.Builder
	for iname, iinfo := range interfaces {
		if len(iinfo.methods) == 0 {
			continue
		}
		var implementors []string
		for sname, sinfo := range structs {
			match := true
			for _, im := range iinfo.methods {
				found := false
				for _, sm := range sinfo.methods {
					if im == sm {
						found = true
						break
					}
				}
				if !found {
					match = false
					break
				}
			}
			if match {
				implementors = append(implementors, sname)
			}
		}
		if len(implementors) > 0 {
			sb.WriteString(fmt.Sprintf("Interface %s (in %s) is implemented by: %s\n", iname, iinfo.path, strings.Join(implementors, ", ")))
		}
	}

	if sb.Len() == 0 {
		return "No interface implementations found.", nil
	}
	return sb.String(), nil
}

func getTypeInfo(args map[string]interface{}) (string, error) {
	typename, _ := args["typename"].(string)
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	fset := token.NewFileSet()
	var sb strings.Builder

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
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
								sb.WriteString(fmt.Sprintf("  - %s %s%s\n", strings.Join(names, ", "), exprToString(field.Type), tag))
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
						filepath.Walk(path, func(p string, i os.FileInfo, e error) error {
							if e != nil || i.IsDir() || filepath.Ext(p) != ".go" {
								return nil
							}
							ff, _ := parser.ParseFile(fset, p, nil, 0)
							for _, d := range ff.Decls {
								if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
									recvType := exprToString(fd.Recv.List[0].Type)
									if strings.TrimPrefix(recvType, "*") == typename {
										sb.WriteString(fmt.Sprintf("  - %s\n", getFuncSignature(fd)))
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
		return "", err
	}
	if sb.Len() == 0 {
		return "Type not found.", nil
	}
	return sb.String(), nil
}

func getProjectSummary(args map[string]interface{}) (string, error) {
	var sb strings.Builder
	sb.WriteString("Project Summary:\n")

	// 1. Go Module Info
	if content, err := os.ReadFile("go.mod"); err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "go ") {
				sb.WriteString(line + "\n")
			}
		}
	}

	// 2. Stats and Packages
	fileCounts := make(map[string]int)
	packages := make(map[string]bool)
	totalLOC := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext == "" {
			ext = "(no ext)"
		}
		fileCounts[ext]++

		if ext == ".go" {
			packages[filepath.Dir(path)] = true
			// Crude LOC count
			if c, err := os.ReadFile(path); err == nil {
				totalLOC += len(strings.Split(string(c), "\n"))
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	sb.WriteString(fmt.Sprintf("\nFile Counts:\n"))
	for ext, count := range fileCounts {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", ext, count))
	}

	sb.WriteString(fmt.Sprintf("\nGo Packages (%d):\n", len(packages)))
	for pkg := range packages {
		sb.WriteString(fmt.Sprintf("  - %s\n", pkg))
	}
	sb.WriteString(fmt.Sprintf("\nEstimated Go LOC: %d\n", totalLOC))

	return sb.String(), nil
}

func searchUsagesGlobally(args map[string]interface{}) (string, error) {
	query, _ := args["query"].(string)
	re, err := regexp.Compile(query)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules" || info.Name() == "output" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip binary files heuristic
		if info.Size() > 1024*1024 { // Skip files > 1MB
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		if isBinary(content) {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				trimmed := strings.TrimSpace(line)
				if len(trimmed) > 500 {
					trimmed = trimmed[:500] + " (truncated)"
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, trimmed))
				if len(results) > 100 {
					return fmt.Errorf("too many results")
				}
			}
		}
		return nil
	})

	if err != nil && err.Error() != "too many results" {
		return "", err
	}

	if len(results) == 0 {
		return "No matches found.", nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return out, nil
}

func semanticDiff(args map[string]interface{}) (string, error) {
	target, _ := args["target"].(string)

	// Get stat summary
	statOut, err := exec.Command("git", "diff", "--stat", target).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff --stat failed: %s", string(statOut))
	}

	// Get summary of changes
	summaryOut, err := exec.Command("git", "diff", "--summary", target).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff --summary failed: %s", string(summaryOut))
	}

	var sb strings.Builder
	sb.WriteString("Semantic Diff Summary:\n\n")
	sb.WriteString("File Statistics:\n")
	sb.WriteString(string(statOut))
	sb.WriteString("\nChange Summary:\n")
	sb.WriteString(string(summaryOut))

	// Try to extract changed Go functions if it's a small diff
	funcDiff, err := exec.Command("git", "diff", "-U0", "--no-color", target).CombinedOutput()
	if err == nil {
		sb.WriteString("\nLogical Changes (Functions):\n")
		lines := strings.Split(string(funcDiff), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "@@") {
				// git diff -U0 includes function name in the hunk header
				parts := strings.SplitN(line, "@@", 3)
				if len(parts) >= 3 {
					funcName := strings.TrimSpace(parts[2])
					if funcName != "" {
						sb.WriteString(fmt.Sprintf("  - %s\n", funcName))
					}
				}
			}
		}
	}

	return sb.String(), nil
}

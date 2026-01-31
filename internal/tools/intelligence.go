// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/types"
	"golang.org/x/tools/imports"
)

type intelligenceManager struct {
	sm *SecurityManager
}

// Global AST Cache to improve performance of AST-based tools
type cachedFile struct {
	file    *ast.File
	modTime time.Time
}

type astCache struct {
	mu      sync.Mutex
	files   map[string]cachedFile
	fset    *token.FileSet
	maxSize int
}

func newASTCache() *astCache {
	return &astCache{
		files:   make(map[string]cachedFile),
		fset:    token.NewFileSet(),
		maxSize: 1000,
	}
}

func (c *astCache) get(path string) (*ast.File, *token.FileSet, error) {
	// 1. Stat the file (I/O) - outside lock
	info, err := os.Stat(path)
	if err != nil {
		return nil, c.fset, err
	}

	// 2. Fast path: Check cache
	c.mu.Lock()
	entry, ok := c.files[path]
	if ok && entry.modTime.Equal(info.ModTime()) {
		c.mu.Unlock()
		return entry.file, c.fset, nil
	}
	c.mu.Unlock()

	// 3. Slow path: Parse without holding lock
	f, err := parser.ParseFile(c.fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, c.fset, err
	}

	// 4. Update cache
	c.mu.Lock()
	defer c.mu.Unlock()

	// Eviction policy
	if len(c.files) >= c.maxSize {
		for k := range c.files {
			delete(c.files, k)
			break
		}
	}

	c.files[path] = cachedFile{
		file:    f,
		modTime: info.ModTime(),
	}

	return f, c.fset, nil
}

var globalASTCache = newASTCache()

// RegisterIntelligenceTools adds AST-based tools to the registry.
func RegisterIntelligenceTools(r *Registry, sm *SecurityManager) {
	m := &intelligenceManager{sm: sm}

	r.Register(&types.ToolDeclaration{
		Name:        "find_usages",
		Description: "Uses static analysis (AST) to find all precise references to a specific Go symbol. Use this for accurate refactoring or impact analysis.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"query": {
					Type:        "STRING",
					Description: "The symbol name to find.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"query"},
		},
	}, m.findUsages)

	r.Register(&types.ToolDeclaration{
		Name:        "list_implementations",
		Description: "Map the relationship between interfaces and structs in the codebase.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
		},
	}, m.listImplementations)

	r.Register(&types.ToolDeclaration{
		Name:        "get_type_info",
		Description: "Provides a detailed structural breakdown of a Go type, including fields, and all associated methods. Use this to understand internal state and behavior.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"typename": {
					Type:        "STRING",
					Description: "The name of the type to inspect.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search for the type (defaults to '.')",
				},
			},
			Required: []string{"typename"},
		},
	}, m.getTypeInfo)

	r.Register(&types.ToolDeclaration{
		Name:        "get_project_summary",
		Description: "Returns a high-level summary of the project architecture, including packages, file counts, and Go module info.",
	}, m.getProjectSummary)

	r.Register(&types.ToolDeclaration{
		Name:        "search_usages_globally",
		Description: "Performs a high-speed text search across all non-ignored project files. Use this for non-code files (YAML, MD) or finding hardcoded strings.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"query": {
					Type:        "STRING",
					Description: "The string or regex to search for.",
				},
			},
			Required: []string{"query"},
		},
	}, m.searchUsagesGlobally)

	r.Register(&types.ToolDeclaration{
		Name:        "semantic_diff",
		Description: "Analyzes Go code changes between the current state and a Git target using AST comparison. Summarizes logical changes rather than raw line diffs.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"target": {
					Type:        "STRING",
					Description: "The git target (commit hash, branch name, or 'HEAD~1') to compare against.",
				},
			},
			Required: []string{"target"},
		},
	}, m.semanticDiff)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "rename_symbol",
		Description: "Safely renames a Go symbol (function, type, variable) across the project using AST.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"old_name": {
					Type:        "STRING",
					Description: "The current name of the symbol.",
				},
				"new_name": {
					Type:        "STRING",
					Description: "The new name for the symbol.",
				},
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
			},
			Required: []string{"old_name", "new_name"},
		},
	}, m.renameSymbol, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "list_todos",
		Description: "Scans the project for TODO, FIXME, or BUG comments.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to scan (defaults to '.')",
				},
			},
		},
	}, m.listTodos)

	r.Register(&types.ToolDeclaration{
		Name:        "go_doc",
		Description: "Retrieves the official Go documentation and comments for a symbol or package. Best for understanding the intended usage of a library.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"symbol": {
					Type:        "STRING",
					Description: "The package or symbol to get documentation for (e.g., 'fmt.Println', './internal/tools').",
				},
			},
			Required: []string{"symbol"},
		},
	}, m.goDoc)

	r.Register(&types.ToolDeclaration{
		Name:        "analyze_complexity",
		Description: "Calculates the cyclomatic complexity of Go functions in a file or directory.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The file or directory to analyze.",
				},
			},
			Required: []string{"path"},
		},
	}, m.analyzeComplexity)

	r.Register(&types.ToolDeclaration{
		Name:        "get_package_graph",
		Description: "Returns a mapping of internal package dependencies.",
	}, m.getPackageGraph)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "move_definition",
		Description: "Moves a Go symbol (struct, interface, function) and its associated methods from one file to another.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"symbol": {
					Type:        "STRING",
					Description: "The name of the symbol to move.",
				},
				"src_file": {
					Type:        "STRING",
					Description: "The source file path.",
				},
				"dst_file": {
					Type:        "STRING",
					Description: "The destination file path.",
				},
			},
			Required: []string{"symbol", "src_file", "dst_file"},
		},
	}, m.moveDefinition, ToolOptions{Serial: true})
}

// AST-based helpers for existing tools

func grepDefinitionsGo(path, query string) ([]string, error) {
	var results []string
	var parseErrors []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, fset, err := globalASTCache.get(filePath)
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

	if len(results) == 0 && len(parseErrors) > 0 {
		return nil, fmt.Errorf("failed to parse Go files:\n%s", strings.Join(parseErrors, "\n"))
	}

	return results, err
}

func getFileSkeletonGo(filePath string) (string, error) {
	f, _, err := globalASTCache.get(filePath)
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

func (m *intelligenceManager) moveDefinition(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Symbol  string `json:"symbol"`
		SrcFile string `json:"src_file"`
		DstFile string `json:"dst_file"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	symbol := params.Symbol
	srcPath := params.SrcFile
	dstPath := params.DstFile

	if err := m.sm.IsPathWritable(srcPath); err != nil {
		return types.ToolResult{}, err
	}
	if err := m.sm.IsPathWritable(dstPath); err != nil {
		return types.ToolResult{}, err
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "MOVE DEFINITION", srcPath, fmt.Sprintf("%s -> %s", symbol, dstPath))
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	fset := token.NewFileSet()
	srcFile, err := parser.ParseFile(fset, srcPath, nil, parser.ParseComments)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to parse source file: %w", err)
	}

	dstFile, err := parser.ParseFile(fset, dstPath, nil, parser.ParseComments)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to infer package name from directory
			pkgName := filepath.Base(filepath.Dir(dstPath))
			// If it's the same directory as src, use src package name
			if filepath.Dir(dstPath) == filepath.Dir(srcPath) {
				pkgName = srcFile.Name.Name
			}
			content := fmt.Sprintf("package %s\n", pkgName)
			if err := os.WriteFile(dstPath, []byte(content), 0644); err != nil {
				return types.ToolResult{}, fmt.Errorf("failed to create destination file: %w", err)
			}
			dstFile, err = parser.ParseFile(fset, dstPath, nil, parser.ParseComments)
			if err != nil {
				return types.ToolResult{}, fmt.Errorf("failed to parse newly created destination file: %w", err)
			}
		} else {
			return types.ToolResult{}, fmt.Errorf("failed to parse destination file: %w", err)
		}
	}

	// Check for name collision in destination
	for _, decl := range dstFile.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == symbol {
					return types.ToolResult{}, fmt.Errorf("symbol '%s' already exists in destination %s", symbol, dstPath)
				}
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						if name.Name == symbol {
							return types.ToolResult{}, fmt.Errorf("symbol '%s' already exists in destination %s", symbol, dstPath)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name == symbol {
				return types.ToolResult{}, fmt.Errorf("symbol '%s' already exists in destination %s", symbol, dstPath)
			}
		}
	}

	var movedDecls []ast.Decl
	var newSrcDecls []ast.Decl
	srcPackageName := srcFile.Name.Name
	dstPackageName := dstFile.Name.Name

	// Identify what to move
	for _, decl := range srcFile.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				newSrcDecls = append(newSrcDecls, d)
				continue
			}
			var keptSpecs []ast.Spec
			var movingSpecs []ast.Spec
			for _, spec := range d.Specs {
				match := false
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == symbol {
						match = true
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if name.Name == symbol {
							match = true
							break
						}
					}
				}

				if match {
					movingSpecs = append(movingSpecs, spec)
				} else {
					keptSpecs = append(keptSpecs, spec)
				}
			}

			if len(movingSpecs) > 0 {
				movedGenDecl := &ast.GenDecl{
					Tok:   d.Tok,
					Specs: movingSpecs,
				}
				if len(movingSpecs) > 1 {
					movedGenDecl.Lparen = d.Lparen
					movedGenDecl.Rparen = d.Rparen
				}
				movedDecls = append(movedDecls, movedGenDecl)
			}
			if len(keptSpecs) > 0 {
				d.Specs = keptSpecs
				if len(keptSpecs) == 1 {
					d.Lparen = 0
					d.Rparen = 0
				}
				newSrcDecls = append(newSrcDecls, d)
			}

		case *ast.FuncDecl:
			shouldMove := false
			if d.Name.Name == symbol {
				shouldMove = true
			} else if d.Recv != nil {
				// Move methods of the symbol if symbol is a type
				recvType := exprToString(d.Recv.List[0].Type)
				if strings.TrimPrefix(recvType, "*") == symbol {
					shouldMove = true
				}
			}

			if shouldMove {
				movedDecls = append(movedDecls, d)
			} else {
				newSrcDecls = append(newSrcDecls, d)
			}
		default:
			newSrcDecls = append(newSrcDecls, decl)
		}
	}

	if len(movedDecls) == 0 {
		return types.ToolResult{Text: fmt.Sprintf("Symbol '%s' not found in %s", symbol, srcPath)}, nil
	}

	// Update source file
	srcFile.Decls = newSrcDecls
	var srcBuf bytes.Buffer
	if err := format.Node(&srcBuf, fset, srcFile); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to format source file: %w", err)
	}
	if err := fsutil.AtomicWrite(srcPath, srcBuf.Bytes(), 0644); err != nil {
		return types.ToolResult{}, err
	}

	dstFile.Decls = append(dstFile.Decls, movedDecls...)

	var dstBuf bytes.Buffer
	if err := format.Node(&dstBuf, fset, dstFile); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to format destination file: %w", err)
	}

	formatted, err := imports.Process(dstPath, dstBuf.Bytes(), nil)
	if err != nil {
		// Fallback to raw formatted content if imports.Process fails
		formatted = dstBuf.Bytes()
	}

	if err := fsutil.AtomicWrite(dstPath, formatted, 0644); err != nil {
		return types.ToolResult{}, err
	}

	if err != nil {
		return types.ToolResult{}, fmt.Errorf("imports processing failed (file written unoptimized): %w", err)
	}

	resultMsg := fmt.Sprintf("Moved '%s' from %s to %s.", symbol, srcPath, dstPath)
	if srcPackageName != dstPackageName {
		resultMsg += " Note: Package names differ. References across the project were NOT updated. Please update them manually or use rename_symbol if applicable."
	}

	return types.ToolResult{Text: resultMsg}, nil
}

func (m *intelligenceManager) renameSymbol(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
		Path    string `json:"path"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	oldName := params.OldName
	newName := params.NewName
	path := params.Path
	if path == "" {
		path = "."
	}

	if err := m.sm.IsPathWritable(path); err != nil {
		return types.ToolResult{}, err
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "RENAME SYMBOL", path, fmt.Sprintf("%s -> %s", oldName, newName))
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	totalFiles := 0
	totalChanges := 0

	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() || filepath.Ext(filePath) != ".go" {
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
			if err := fsutil.AtomicWrite(filePath, buf.Bytes(), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", filePath, err)
			}
		}
		return nil
	})

	if totalChanges == 0 {
		return types.ToolResult{Text: fmt.Sprintf("Symbol '%s' not found.", oldName)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Renamed %d occurrences of '%s' to '%s' in %d files.", totalChanges, oldName, newName, totalFiles)}, err
}

func (m *intelligenceManager) listTodos(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
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
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No TODOs, FIXMEs, or BUGs found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *intelligenceManager) goDoc(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Symbol string `json:"symbol"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	symbol := params.Symbol
	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Running go doc %s\033[0m\n", symbol)
	}()

	cmd := exec.CommandContext(ctx, "go", "doc", symbol)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error running go doc: %v\nOutput: %s", err, string(out))}, nil
	}

	return types.ToolResult{Text: string(out)}, nil
}

func (m *intelligenceManager) analyzeComplexity(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	var results []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, fset, err := globalASTCache.get(filePath)
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

func (m *intelligenceManager) getPackageGraph(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Analyzing package dependencies\033[0m\n")
	}()

	cmd := exec.CommandContext(ctx, "go", "list", "-f", "{{.ImportPath}} -> {{.Imports}}", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error listing packages: %v\nOutput: %s", err, string(out))}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var sb strings.Builder
	sb.WriteString("Internal Package Dependency Graph:\n")

	// Get module name to filter for internal imports
	modCmd := exec.CommandContext(ctx, "go", "list", "-m")
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

	return types.ToolResult{Text: sb.String()}, nil
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

func (m *intelligenceManager) findUsages(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	query := params.Query
	path := params.Path
	if path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	var results []string

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}

		f, fset, err := globalASTCache.get(filePath)
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
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No usages found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *intelligenceManager) listImplementations(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

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
		f, _, err := globalASTCache.get(filePath)
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
		f, _, err := globalASTCache.get(filePath)
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
		return types.ToolResult{Text: "No interface implementations found."}, nil
	}
	return types.ToolResult{Text: sb.String()}, nil
}

func (m *intelligenceManager) getTypeInfo(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Typename string `json:"typename"`
		Path     string `json:"path"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	typename := params.Typename
	path := params.Path
	if path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	var sb strings.Builder

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, _, err := globalASTCache.get(filePath)
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
							ff, _, _ := globalASTCache.get(p)
							if ff == nil {
								return nil
							}
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
		return types.ToolResult{}, err
	}
	if sb.Len() == 0 {
		return types.ToolResult{Text: "Type not found."}, nil
	}
	return types.ToolResult{Text: sb.String()}, nil
}

func (m *intelligenceManager) getProjectSummary(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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
		return types.ToolResult{}, err
	}

	sb.WriteString("\nFile Counts:\n")
	for ext, count := range fileCounts {
		sb.WriteString(fmt.Sprintf("  %s: %d\n", ext, count))
	}

	sb.WriteString(fmt.Sprintf("\nGo Packages (%d):\n", len(packages)))
	for pkg := range packages {
		sb.WriteString(fmt.Sprintf("  - %s\n", pkg))
	}
	sb.WriteString(fmt.Sprintf("\nEstimated Go LOC: %d\n", totalLOC))

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *intelligenceManager) searchUsagesGlobally(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	query := params.Query
	re, err := regexp.Compile(query)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No matches found."}, nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return types.ToolResult{Text: out}, nil
}

func (m *intelligenceManager) semanticDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Target string `json:"target"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	target := params.Target

	// 1. Get stats and summary as before
	statOut, _ := exec.CommandContext(ctx, "git", "diff", "--stat", target).CombinedOutput()
	summaryOut, _ := exec.CommandContext(ctx, "git", "diff", "--summary", target).CombinedOutput()

	var sb strings.Builder
	sb.WriteString("Semantic Diff Summary:\n\n")
	sb.WriteString("File Statistics:\n")
	sb.WriteString(string(statOut))
	sb.WriteString("\nChange Summary:\n")
	sb.WriteString(string(summaryOut))

	// 2. Logical Analysis
	sb.WriteString("\nLogical Code Changes:\n")

	// Get list of changed .go files
	filesOut, err := exec.CommandContext(ctx, "git", "diff", "--name-only", target).CombinedOutput()
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
		currAST, _, err := globalASTCache.get(relPath)
		if err != nil {
			continue // Skip unparsable current files
		}

		// Get target AST (base)
		var baseAST *ast.File
		baseContent, err := exec.CommandContext(ctx, "git", "show", target+":"+relPath).Output()
		if err == nil {
			baseAST, _ = parser.ParseFile(fset, relPath, baseContent, parser.ParseComments)
		}

		var changes []string
		if baseAST == nil {
			// Entirely new file
			for _, d := range currAST.Decls {
				key := getDeclKey(d)
				if key != "unknown" {
					changes = append(changes, "Added: "+key)
				}
			}
		} else {
			changes = compareASTs(baseAST, currAST)
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

func compareASTs(base, curr *ast.File) []string {
	var changes []string

	baseDecls := map[string]ast.Decl{}
	for _, d := range base.Decls {
		baseDecls[getDeclKey(d)] = d
	}

	currDecls := map[string]ast.Decl{}
	for _, d := range curr.Decls {
		currDecls[getDeclKey(d)] = d
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
			if !isDeclEqual(baseDecl, currDecl) {
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

func getDeclKey(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		name := d.Name.Name
		if d.Recv != nil && len(d.Recv.List) > 0 {
			recv := exprToString(d.Recv.List[0].Type)
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

func isDeclEqual(a, b ast.Decl) bool {
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

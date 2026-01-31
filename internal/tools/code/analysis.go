// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

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

	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type AnalysisManager struct {
	SP types.SecurityProvider
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

		f, fset, err := GlobalCache.Get(filePath)
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
					recvType := ExprToString(fd.Recv.List[0].Type)
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

func (m *AnalysisManager) SemanticDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Target string `json:"target"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
		currAST, _, err := GlobalCache.Get(relPath)
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
			recv := ExprToString(d.Recv.List[0].Type)
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

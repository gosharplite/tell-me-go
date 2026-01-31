// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package refactor

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/tools/astutil"
	"github.com/gosharplite/tell-me-go/internal/types"
	"golang.org/x/tools/imports"
)

type Manager struct {
	SP types.SecurityProvider
}

func (m *Manager) MoveDefinition(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Symbol  string `json:"symbol"`
		SrcFile string `json:"src_file"`
		DstFile string `json:"dst_file"`
	}
	if err := types.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	symbol := params.Symbol
	srcPath := params.SrcFile
	dstPath := params.DstFile

	resolvedSrc, err := m.SP.IsPathWritable(srcPath)
	if err != nil {
		return types.ToolResult{}, err
	}
	resolvedDst, err := m.SP.IsPathWritable(dstPath)
	if err != nil {
		return types.ToolResult{}, err
	}

	approved, err := m.SP.ConfirmDestructiveAction(ctx, "MOVE DEFINITION", resolvedSrc, fmt.Sprintf("%s -> %s", symbol, resolvedDst))
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	fset := token.NewFileSet()
	srcFile, err := parser.ParseFile(fset, resolvedSrc, nil, parser.ParseComments)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to parse source file: %w", err)
	}

	dstFile, err := parser.ParseFile(fset, resolvedDst, nil, parser.ParseComments)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to infer package name from directory
			pkgName := filepath.Base(filepath.Dir(resolvedDst))
			// If it's the same directory as src, use src package name
			if filepath.Dir(resolvedDst) == filepath.Dir(resolvedSrc) {
				pkgName = srcFile.Name.Name
			}
			content := fmt.Sprintf("package %s\n", pkgName)
			if err := os.WriteFile(resolvedDst, []byte(content), 0644); err != nil {
				return types.ToolResult{}, fmt.Errorf("failed to create destination file: %w", err)
			}
			dstFile, err = parser.ParseFile(fset, resolvedDst, nil, parser.ParseComments)
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
					return types.ToolResult{}, fmt.Errorf("symbol '%s' already exists in destination %s", symbol, resolvedDst)
				}
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range vs.Names {
						if name.Name == symbol {
							return types.ToolResult{}, fmt.Errorf("symbol '%s' already exists in destination %s", symbol, resolvedDst)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name == symbol {
				return types.ToolResult{}, fmt.Errorf("symbol '%s' already exists in destination %s", symbol, resolvedDst)
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
				recvType := astutil.ExprToString(d.Recv.List[0].Type)
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
		return types.ToolResult{Text: fmt.Sprintf("Symbol '%s' not found in %s", symbol, resolvedSrc)}, nil
	}

	// Update source file
	srcFile.Decls = newSrcDecls
	var srcBuf bytes.Buffer
	if err := format.Node(&srcBuf, fset, srcFile); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to format source file: %w", err)
	}
	if err := fsutil.AtomicWrite(ctx, resolvedSrc, srcBuf.Bytes(), 0644); err != nil {
		return types.ToolResult{}, err
	}

	dstFile.Decls = append(dstFile.Decls, movedDecls...)

	var dstBuf bytes.Buffer
	if err := format.Node(&dstBuf, fset, dstFile); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to format destination file: %w", err)
	}

	formatted := dstBuf.Bytes()
	if opt, err := imports.Process(resolvedDst, formatted, nil); err == nil {
		formatted = opt
	}

	if err := fsutil.AtomicWrite(ctx, resolvedDst, formatted, 0644); err != nil {
		return types.ToolResult{}, err
	}

	resultMsg := fmt.Sprintf("Moved '%s' from %s to %s.", symbol, resolvedSrc, resolvedDst)
	if srcPackageName != dstPackageName {
		resultMsg += " Note: Package names differ. References across the project were NOT updated. Please update them manually or use rename_symbol if applicable."
	}

	return types.ToolResult{Text: resultMsg}, nil
}

func (m *Manager) RenameSymbol(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
		Path    string `json:"path"`
	}
	if err := types.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	oldName := params.OldName
	newName := params.NewName
	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.SP.IsPathWritable(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	approved, err := m.SP.ConfirmDestructiveAction(ctx, "RENAME SYMBOL", resolvedPath, fmt.Sprintf("%s -> %s", oldName, newName))
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	totalFiles := 0
	totalChanges := 0

	err = filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
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
			if err := fsutil.AtomicWrite(ctx, filePath, buf.Bytes(), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", filePath, err)
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}

	if totalChanges == 0 {
		return types.ToolResult{Text: fmt.Sprintf("Symbol '%s' not found.", oldName)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Renamed %d occurrences of '%s' to '%s' in %d files.", totalChanges, oldName, newName, totalFiles)}, nil
}

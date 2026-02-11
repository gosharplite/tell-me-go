// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Global AST Cache to improve performance of AST-based tools
type cachedFile struct {
	file    *ast.File
	fset    *token.FileSet
	modTime time.Time
}

type astCache struct {
	mu      sync.RWMutex
	files   map[string]cachedFile
	sf      singleflight.Group
	maxSize int
}

func newASTCache() *astCache {
	return &astCache{
		files:   make(map[string]cachedFile),
		maxSize: 1000,
	}
}

func (c *astCache) Get(path string) (*ast.File, *token.FileSet, error) {
	// 1. Fast path: Check cache with RLock
	c.mu.RLock()
	entry, ok := c.files[path]
	if ok {
		// Check if still valid (Stat is fast)
		info, err := os.Stat(path)
		if err == nil && entry.modTime.Equal(info.ModTime()) {
			c.mu.RUnlock()
			return entry.file, entry.fset, nil
		}
	}
	c.mu.RUnlock()

	// 2. Slow path: Use singleflight to deduplicate concurrent requests for the same path
	res, err, _ := c.sf.Do(path, func() (interface{}, error) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		// Parse using a local FileSet to allow full concurrency across DIFFERENT files.
		// Since FileSet.AddFile is the only non-thread-safe part of parsing,
		// using a local FileSet here removes the bottleneck entirely.
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		defer c.mu.Unlock()

		// Eviction policy
		if len(c.files) >= c.maxSize {
			for k := range c.files {
				delete(c.files, k)
				break
			}
		}

		newEntry := cachedFile{
			file:    f,
			fset:    fset,
			modTime: info.ModTime(),
		}
		c.files[path] = newEntry

		return newEntry, nil
	})

	if err != nil {
		return nil, nil, err
	}

	cf := res.(cachedFile)
	return cf.file, cf.fset, nil
}

func ExprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return ExprToString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + ExprToString(t.X)
	case *ast.ArrayType:
		return "[]" + ExprToString(t.Elt)
	case *ast.MapType:
		return "map[" + ExprToString(t.Key) + "]" + ExprToString(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + ExprToString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	default:
		return fmt.Sprintf("%T", t)
	}
}

func writeFields(sb *strings.Builder, fields *ast.FieldList, showNames bool) {
	for i, field := range fields.List {
		if i > 0 {
			sb.WriteString(", ")
		}
		if showNames && len(field.Names) > 0 {
			for j, name := range field.Names {
				if j > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(name.Name)
			}
			sb.WriteString(" ")
		}
		sb.WriteString(ExprToString(field.Type))
	}
}

func writeResults(sb *strings.Builder, results *ast.FieldList) {
	if len(results.List) > 1 {
		sb.WriteString("(")
	}
	writeFields(sb, results, false)
	if len(results.List) > 1 {
		sb.WriteString(")")
	}
}

func GetFuncSignature(f *ast.FuncDecl) string {
	var sb strings.Builder
	sb.WriteString("func ")
	if f.Recv != nil {
		sb.WriteString("(")
		writeFields(&sb, f.Recv, true)
		sb.WriteString(") ")
	}
	sb.WriteString(f.Name.Name + "(")
	if f.Type.Params != nil {
		writeFields(&sb, f.Type.Params, true)
	}
	sb.WriteString(")")
	if f.Type.Results != nil {
		sb.WriteString(" ")
		writeResults(&sb, f.Type.Results)
	}
	return sb.String()
}

func CalculateComplexity(fd *ast.FuncDecl) int {
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
	return complexity
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

// FindTypeSpec searches for a type specification by name in an AST file.
func FindTypeSpec(f *ast.File, name string) (*ast.TypeSpec, *ast.GenDecl) {
	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, spec := range gd.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == name {
					return ts, gd
				}
			}
		}
	}
	return nil, nil
}

// GetFileSkeletonGo extracts exported types and function signatures from a Go file.
func (c *astCache) GetFileSkeletonGo(filePath string) (string, error) {
	f, fset, err := c.Get(filePath)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("package %s\n\n", f.Name.Name))

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					ts := spec.(*ast.TypeSpec)
					if ts.Name.IsExported() {
						// Create a copy of GenDecl for formatting
						newGD := &ast.GenDecl{
							Doc:    d.Doc,
							TokPos: d.TokPos,
							Tok:    d.Tok,
							Lparen: d.Lparen,
							Specs:  []ast.Spec{ts},
							Rparen: d.Rparen,
						}
						if err := format.Node(&sb, fset, newGD); err == nil {
							sb.WriteString("\n\n")
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				// Create a copy of FuncDecl for formatting
				newFD := &ast.FuncDecl{
					Doc:  d.Doc,
					Recv: d.Recv,
					Name: d.Name,
					Type: d.Type,
					Body: nil, // Remove body
				}
				if err := format.Node(&sb, fset, newFD); err == nil {
					sb.WriteString("\n\n")
				}
			}
		}
	}

	return strings.TrimSpace(sb.String()), nil
}

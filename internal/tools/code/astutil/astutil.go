// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package astutil

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
)

// Global AST Cache to improve performance of AST-based tools
type cachedFile struct {
	file    *ast.File
	modTime time.Time
}

type ASTCache struct {
	mu      sync.Mutex
	files   map[string]cachedFile
	fset    *token.FileSet
	maxSize int
}

func NewASTCache() *ASTCache {
	return &ASTCache{
		files:   make(map[string]cachedFile),
		fset:    token.NewFileSet(),
		maxSize: 1000,
	}
}

func (c *ASTCache) Get(path string) (*ast.File, *token.FileSet, error) {
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

func GetFuncSignature(f *ast.FuncDecl) string {
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
			sb.WriteString(ExprToString(field.Type))
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
			sb.WriteString(" " + ExprToString(field.Type))
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
			sb.WriteString(ExprToString(field.Type))
		}
		if len(f.Type.Results.List) > 1 {
			sb.WriteString(")")
		}
	}
	return sb.String()
}

func GetFuncTypeSig(f *ast.FuncType) string {
	var sb strings.Builder
	sb.WriteString("(")
	if f.Params != nil {
		for i, field := range f.Params.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(ExprToString(field.Type))
		}
	}
	sb.WriteString(")")
	if f.Results != nil {
		sb.WriteString(" ")
		if len(f.Results.List) > 1 {
			sb.WriteString("(")
		}
		for i, field := range f.Results.List {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(ExprToString(field.Type))
		}
		if len(f.Results.List) > 1 {
			sb.WriteString(")")
		}
	}
	return sb.String()
}

func (c *ASTCache) GetFileSkeletonGo(filePath string) (string, error) {
	f, _, err := c.Get(filePath)
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
			sb.WriteString(GetFuncSignature(d) + "\n\n")
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

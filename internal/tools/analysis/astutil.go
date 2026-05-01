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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Global AST Cache to improve performance of AST-based tools
type cachedFile struct {
	file      *ast.File
	fset      *token.FileSet
	modTime   time.Time
	lineCount int
}

type astCache struct {
	baseDir string // injected once; all paths resolved relative to this root
	mu      sync.RWMutex
	files   map[string]cachedFile
	order   []string
	sf      singleflight.Group
	maxSize int
}

func newASTCache(baseDir string) *astCache {
	return &astCache{
		baseDir: baseDir,
		files:   make(map[string]cachedFile),
		order:   make([]string, 0),
		maxSize: 1000,
	}
}

func (c *astCache) absPath(relPath string) string {
	if c.baseDir == "" || filepath.IsAbs(relPath) {
		return relPath
	}
	return filepath.Join(c.baseDir, relPath)
}

func (cf cachedFile) isValid(info os.FileInfo) bool {
	return cf.modTime.Equal(info.ModTime())
}

func (c *astCache) GetCachedLineCount(path string, info os.FileInfo) (int, bool) {
	abs := c.absPath(path)
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.files[abs]
	if ok && entry.isValid(info) {
		return entry.lineCount, true
	}
	return 0, false
}

func (c *astCache) getValidEntry(path string) (cachedFile, bool) {
	abs := c.absPath(path)
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.files[abs]
	if !ok {
		return cachedFile{}, false
	}
	info, err := os.Stat(abs)
	if err != nil || !entry.modTime.Equal(info.ModTime()) {
		return cachedFile{}, false
	}
	return entry, true
}

func (c *astCache) updateCache(path string, info os.FileInfo, f *ast.File, fset *token.FileSet) cachedFile {
	abs := c.absPath(path)
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.files[abs]
	// Eviction policy: FIFO
	if !exists && len(c.files) >= c.maxSize {
		if len(c.order) > 0 {
			victim := c.order[0]
			c.order = c.order[1:]
			delete(c.files, victim)
		}
	}
	newEntry := cachedFile{
		file:    f,
		fset:    fset,
		modTime: info.ModTime(),
	}
	if fset != nil && f != nil {
		if tf := fset.File(f.Pos()); tf != nil {
			newEntry.lineCount = tf.LineCount()
		}
	}
	c.files[abs] = newEntry
	if !exists {
		c.order = append(c.order, abs)
	}
	return newEntry
}

func (c *astCache) Get(path string) (*ast.File, *token.FileSet, error) {
	abs := c.absPath(path)
	// 1. Fast path: Check cache
	if entry, ok := c.getValidEntry(abs); ok {
		return entry.file, entry.fset, nil
	}
	// 2. Slow path: singleflight with absolute key
	res, err, _ := c.sf.Do(abs, func() (interface{}, error) {
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, abs, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		return c.updateCache(abs, info, f, fset), nil
	})
	if err != nil {
		return nil, nil, err
	}
	cf := res.(cachedFile)
	return cf.file, cf.fset, nil
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
	}
	return handleComplexExpr(expr)
}

func handleComplexExpr(expr ast.Expr) string {
	return fmt.Sprintf("%T", expr)
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
		sb.WriteString(exprToString(field.Type))
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

func getFuncSignature(f *ast.FuncDecl) string {
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

func calculateComplexity(fd *ast.FuncDecl) int {
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
	changes = append(changes, findAddedAndModified(baseDecls, currDecls)...)

	// Find Deleted
	changes = append(changes, findDeleted(baseDecls, currDecls)...)

	return changes
}

func findAddedAndModified(baseDecls, currDecls map[string]ast.Decl) []string {
	var changes []string
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
	return changes
}

func findDeleted(baseDecls, currDecls map[string]ast.Decl) []string {
	var changes []string
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
		return handleFuncDeclKey(d)
	case *ast.GenDecl:
		return handleGenDeclKey(d)
	}
	return "unknown"
}

func handleFuncDeclKey(d *ast.FuncDecl) string {
	name := d.Name.Name
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := exprToString(d.Recv.List[0].Type)
		return fmt.Sprintf("func (%s) %s", recv, name)
	}
	return "func " + name
}

func handleGenDeclKey(d *ast.GenDecl) string {
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

// findTypeSpec searches for a type specification by name in an AST file.
func findTypeSpec(f *ast.File, name string) (*ast.TypeSpec, *ast.GenDecl) {
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
// filePath must be relative to the cache's baseDir.
func (c *astCache) GetFileSkeletonGo(filePath string) (string, error) {
	abs := c.absPath(filePath)
	f, fset, err := c.Get(abs)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "package %s\n\n", f.Name.Name)

	for _, decl := range f.Decls {
		c.writeSkeletonDecl(&sb, fset, decl)
	}

	return strings.TrimSpace(sb.String()), nil
}

func (c *astCache) writeSkeletonDecl(sb *strings.Builder, fset *token.FileSet, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.GenDecl:
		c.writeGenDeclSkeleton(sb, fset, d)
	case *ast.FuncDecl:
		c.writeFuncDeclSkeleton(sb, fset, d)
	}
}

func (c *astCache) writeGenDeclSkeleton(sb *strings.Builder, fset *token.FileSet, d *ast.GenDecl) {
	if d.Tok != token.TYPE {
		return
	}
	for _, spec := range d.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !ts.Name.IsExported() {
			continue
		}
		// Create a copy of GenDecl for formatting
		newGD := &ast.GenDecl{
			Doc:    d.Doc,
			TokPos: d.TokPos,
			Tok:    d.Tok,
			Lparen: d.Lparen,
			Specs:  []ast.Spec{ts},
			Rparen: d.Rparen,
		}
		if err := format.Node(sb, fset, newGD); err == nil {
			sb.WriteString("\n\n")
		}
	}
}

func (c *astCache) writeFuncDeclSkeleton(sb *strings.Builder, fset *token.FileSet, d *ast.FuncDecl) {
	if !d.Name.IsExported() {
		return
	}
	// Create a copy of FuncDecl for formatting
	newFD := &ast.FuncDecl{
		Doc:  d.Doc,
		Recv: d.Recv,
		Name: d.Name,
		Type: d.Type,
		Body: nil, // Remove body
	}
	if err := format.Node(sb, fset, newFD); err == nil {
		sb.WriteString("\n\n")
	}
}

// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type NavigationManager struct {
	SP types.SecurityProvider
}

func (m *NavigationManager) FindUsages(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

		f, fset, err := GlobalCache.Get(filePath)
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

func (m *NavigationManager) FindDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

func (m *NavigationManager) ListSymbols(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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
		f, fset, err := GlobalCache.Get(filePath)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if params.ExportedOnly && !ast.IsExported(d.Name.Name) {
					continue
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, fset.Position(d.Pos()).Line, GetFuncSignature(d)))
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

func (m *NavigationManager) ListImplementations(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
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

	type methodSig struct {
		name string
		sig  string
	}

	type interfaceInfo struct {
		methods []methodSig
		path    string
	}
	interfaces := make(map[string]interfaceInfo)

	type structInfo struct {
		methods []methodSig
		path    string
	}
	structs := make(map[string]structInfo)

	// Phase 1: Collect all interfaces and structs
	filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, _, err := GlobalCache.Get(filePath)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					if it, ok := ts.Type.(*ast.InterfaceType); ok {
						var methods []methodSig
						if it.Methods != nil {
							for _, m := range it.Methods.List {
								if len(m.Names) > 0 {
									if ft, ok := m.Type.(*ast.FuncType); ok {
										methods = append(methods, methodSig{
											name: m.Names[0].Name,
											sig:  GetFuncTypeSig(ft),
										})
									}
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
	filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil || info.IsDir() || filepath.Ext(filePath) != ".go" {
			return nil
		}
		f, _, err := GlobalCache.Get(filePath)
		if err != nil {
			return nil
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil {
				recvType := ExprToString(fd.Recv.List[0].Type)
				recvType = strings.TrimPrefix(recvType, "*")
				if info, ok := structs[recvType]; ok {
					info.methods = append(info.methods, methodSig{
						name: fd.Name.Name,
						sig:  GetFuncTypeSig(fd.Type),
					})
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
					if im.name == sm.name && im.sig == sm.sig {
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

func (m *NavigationManager) GetTypeInfo(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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
		f, _, err := GlobalCache.Get(filePath)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts := spec.(*ast.TypeSpec)
					if ts.Name.Name == typename {
						sb.WriteString(fmt.Sprintf("Type: %s\\nLocation: %s\\n", typename, filePath))
						if gd.Doc != nil {
							sb.WriteString("Doc: " + gd.Doc.Text())
						}
						switch t := ts.Type.(type) {
						case *ast.StructType:
							sb.WriteString("Fields:\\n")
							for _, field := range t.Fields.List {
								names := []string{}
								for _, n := range field.Names {
									names = append(names, n.Name)
								}
								tag := ""
								if field.Tag != nil {
									tag = " " + field.Tag.Value
								}
								sb.WriteString(fmt.Sprintf("  - %s %s%s\\n", strings.Join(names, ", "), ExprToString(field.Type), tag))
							}
						case *ast.InterfaceType:
							sb.WriteString("Methods:\\n")
							for _, m := range t.Methods.List {
								if len(m.Names) > 0 {
									sb.WriteString(fmt.Sprintf("  - %s\\n", m.Names[0].Name))
								}
							}
						}

						// Find methods
						sb.WriteString("Methods (Receivers):\\n")
						filepath.Walk(resolvedPath, func(p string, i os.FileInfo, e error) error {
							select {
							case <-ctx.Done():
								return ctx.Err()
							default:
							}
							if e != nil || i.IsDir() || filepath.Ext(p) != ".go" {
								return nil
							}
							ff, _, _ := GlobalCache.Get(p)
							if ff == nil {
								return nil
							}
							for _, d := range ff.Decls {
								if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
									recvType := ExprToString(fd.Recv.List[0].Type)
									if strings.TrimPrefix(recvType, "*") == typename {
										sb.WriteString(fmt.Sprintf("  - %s\\n", GetFuncSignature(fd)))
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

func (m *NavigationManager) GrepDefinitionsGo(ctx context.Context, path, query string) ([]string, error) {
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

		f, fset, err := GlobalCache.Get(filePath)
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
					sig := GetFuncSignature(d)
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
		return nil, fmt.Errorf("failed to parse Go files:\\n%s", strings.Join(parseErrors, "\\n"))
	}

	return results, err
}

// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

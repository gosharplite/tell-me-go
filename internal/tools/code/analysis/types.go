package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type TypeManager struct {
	Indexer index.SymbolIndex
	Cache   *astutil.ASTCache
	SP      types.SecurityProvider
}

type TypeDefinition struct {
	Name     string
	Doc      string
	Kind     string // "struct", "interface", "alias"
	Fields   []FieldInfo
	Methods  []string // Used for interface methods and receiver methods
	Location string
}

type FieldInfo struct {
	Names string
	Type  string
	Tag   string
}

func NewTypeManager(idx index.SymbolIndex, cache *astutil.ASTCache, sp types.SecurityProvider) *TypeManager {
	return &TypeManager{
		Indexer: idx,
		Cache:   cache,
		SP:      sp,
	}
}

func (m *TypeManager) GetTypeInfo(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Typename string `json:"typename"`
		Path     string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	typename := params.Typename
	if typename == "" {
		return types.ToolResult{Text: "Please provide a typename."}, nil
	}

	if err := m.Indexer.Refresh(ctx); err != nil {
		return types.ToolResult{}, err
	}

	locs, err := m.Indexer.Lookup(ctx, typename)
	if err != nil || len(locs) == 0 {
		return types.ToolResult{Text: "Type not found."}, nil
	}

	// For now, take the first definition
	loc := locs[0]
	f, _, err := m.Cache.Get(loc.Path)
	if err != nil {
		return types.ToolResult{}, err
	}

	ts, gd := astutil.FindTypeSpec(f, typename)
	if ts == nil {
		return types.ToolResult{Text: "Type not found."}, nil
	}

	def := m.extractDefinition(ts, gd, loc)
	receivers, err := m.findMethodsInPackage(filepath.Dir(loc.Path), typename)
	if err != nil {
		return types.ToolResult{}, err
	}

	return types.ToolResult{Text: m.renderTypeInfo(def, receivers)}, nil
}

func (m *TypeManager) ListSymbols(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

	if err := m.Indexer.Refresh(ctx); err != nil {
		return types.ToolResult{}, err
	}

	symbols, err := m.Indexer.SearchSymbols(ctx, resolvedPath, "", params.ExportedOnly)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string
	for _, sym := range symbols {
		desc := sym.Name
		switch sym.Kind {
		case "type":
			desc = "type " + sym.Name
		case "func":
			if sym.Signature != "" {
				desc = sym.Signature
			}
		case "var":
			desc = "var " + sym.Name
		case "const":
			desc = "const " + sym.Name
		}
		results = append(results, fmt.Sprintf("%s:%d: %s", sym.Path, sym.Line, desc))
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No symbols found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *TypeManager) ListImplementations(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		InterfaceName string `json:"interface_name"`
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

func (m *TypeManager) FindUsages(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

	if err := m.Indexer.Refresh(ctx); err != nil {
		return types.ToolResult{}, err
	}

	locs, err := m.Indexer.GetUsages(ctx, query, resolvedPath)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string
	for _, loc := range locs {
		// To keep rendering consistent with previous version, we might want line content.
		// But index only has location. We could fetch line content from cache if needed.
		results = append(results, fmt.Sprintf("%s:%d:%d", loc.Path, loc.Line, loc.Column))
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No usages found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *TypeManager) FindDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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

	if err := m.Indexer.Refresh(ctx); err != nil {
		return types.ToolResult{}, err
	}

	symbols, err := m.Indexer.SearchSymbols(ctx, resolvedPath, params.Query, false)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string
	for _, sym := range symbols {
		desc := sym.Name
		switch sym.Kind {
		case "type":
			desc = "type " + sym.Name
		case "func":
			if sym.Signature != "" {
				desc = sym.Signature
			}
		case "var":
			desc = "var " + sym.Name
		case "const":
			desc = "const " + sym.Name
		}
		results = append(results, fmt.Sprintf("%s:%d: %s", sym.Path, sym.Line, desc))
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No definitions found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *TypeManager) extractDefinition(ts *ast.TypeSpec, gd *ast.GenDecl, loc index.Location) TypeDefinition {
	def := TypeDefinition{
		Name:     ts.Name.Name,
		Location: fmt.Sprintf("%s:%d", loc.Path, loc.Line),
	}
	if gd.Doc != nil {
		def.Doc = gd.Doc.Text()
	}

	switch t := ts.Type.(type) {
	case *ast.StructType:
		def.Kind = "struct"
		def.Fields = m.parseFields(t.Fields)
	case *ast.InterfaceType:
		def.Kind = "interface"
		def.Methods = m.parseInterfaceMethods(t.Methods)
	default:
		def.Kind = "alias"
	}
	return def
}

func (m *TypeManager) parseFields(list *ast.FieldList) []FieldInfo {
	if list == nil {
		return nil
	}
	var fields []FieldInfo
	for _, field := range list.List {
		names := []string{}
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
		tag := ""
		if field.Tag != nil {
			tag = field.Tag.Value
		}
		fields = append(fields, FieldInfo{
			Names: strings.Join(names, ", "),
			Type:  astutil.ExprToString(field.Type),
			Tag:   tag,
		})
	}
	return fields
}

func (m *TypeManager) parseInterfaceMethods(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}
	var methods []string
	for _, m := range list.List {
		if len(m.Names) > 0 {
			methods = append(methods, m.Names[0].Name)
		}
	}
	return methods
}

func (m *TypeManager) findMethodsInPackage(dir, typeName string) ([]string, error) {
	// Still using findMethodsInPackage with Walk/Cache because Indexer currently doesn't 
	// associate methods with types in a way that's easy to retrieve here.
	// Future optimization: Indexer should store receiver types for functions.
	var methods []string
	err := filepath.Walk(dir, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() || filepath.Ext(p) != ".go" {
			return nil
		}
		ff, _, err := m.Cache.Get(p)
		if err != nil {
			return nil
		}
		for _, d := range ff.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
				recvType := astutil.ExprToString(fd.Recv.List[0].Type)
				if strings.TrimPrefix(recvType, "*") == typeName {
					methods = append(methods, astutil.GetFuncSignature(fd))
				}
			}
		}
		return nil
	})
	return methods, err
}

func (m *TypeManager) renderTypeInfo(def TypeDefinition, receivers []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Type: %s\nLocation: %s\n", def.Name, def.Location))
	if def.Kind != "" {
		sb.WriteString(fmt.Sprintf("Kind: %s\n", def.Kind))
	}
	if def.Doc != "" {
		sb.WriteString("Doc: " + def.Doc)
	}

	if len(def.Fields) > 0 {
		sb.WriteString("Fields:\n")
		for _, f := range def.Fields {
			tag := ""
			if f.Tag != "" {
				tag = " " + f.Tag
			}
			sb.WriteString(fmt.Sprintf("  - %s %s%s\n", f.Names, f.Type, tag))
		}
	}

	if len(def.Methods) > 0 {
		sb.WriteString("Methods:\n")
		for _, meth := range def.Methods {
			sb.WriteString(fmt.Sprintf("  - %s\n", meth))
		}
	}

	sb.WriteString("Methods (Receivers):\n")
	for _, meth := range receivers {
		sb.WriteString(fmt.Sprintf("  - %s\n", meth))
	}

	return sb.String()
}

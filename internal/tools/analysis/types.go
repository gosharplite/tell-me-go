package analysis

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type defaultTypeManager struct {
	Indexer symbolIndex
	Cache   *astCache
	SP      security.PathValidator
	fs      persistence.FileSystem
}

type typeDefinition struct {
	Name     string
	Doc      string
	Kind     string // "struct", "interface", "alias"
	Fields   []fieldInfo
	Methods  []string // Used for interface methods and receiver methods
	location string
}

type fieldInfo struct {
	Names string
	Type  string
	Tag   string
}

// NewTypeManager creates a new defaultTypeManager.
// This is the exported constructor for use by sub-packages (e.g., analysistest, analysis_test).
func NewTypeManager(idx SymbolIndex, cache *astCache, sp security.PathValidator, fs persistence.FileSystem) *defaultTypeManager {
	return newTypeManager(idx, cache, sp, fs)
}

func newTypeManager(idx symbolIndex, cache *astCache, sp security.PathValidator, fs persistence.FileSystem) *defaultTypeManager {
	return &defaultTypeManager{
		Indexer: idx,
		Cache:   cache,
		SP:      sp,
		fs:      fs,
	}
}

func (m *defaultTypeManager) GetTypeInfo(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Typename string `json:"typename"`
		Path     string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	typename := params.Typename
	if typename == "" {
		return tools.ToolResult{Text: "Please provide a typename."}, nil
	}

	locs, err := m.Indexer.Lookup(ctx, typename, hb)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("lookup type %s: %w", typename, err)
	}
	if len(locs) == 0 {
		return tools.ToolResult{Text: "Type not found."}, nil
	}

	// For now, take the first definition
	loc := locs[0]
	f, _, err := m.Cache.Get(loc.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	ts, gd := findTypeSpec(f, typename)
	if ts == nil {
		return tools.ToolResult{Text: "Type not found."}, nil
	}

	def := m.extractDefinition(ts, gd, loc)
	receivers, err := m.findMethodsInPackage(ctx, filepath.Dir(loc.Path), typename, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: m.renderTypeInfo(def, receivers)}, nil
}

func (m *defaultTypeManager) ListSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path         string `json:"path"`
		ExportedOnly bool   `json:"exported_only"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resolvedPath, err := m.resolvePath(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	results, err := m.collectSymbols(ctx, resolvedPath, "", params.ExportedOnly, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return m.wrapResults(results, "No symbols found."), nil
}

func (m *defaultTypeManager) ListImplementations(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		InterfaceName string `json:"interface_name"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.InterfaceName == "" {
		return tools.ToolResult{Text: "Please provide an interface_name."}, nil
	}

	implementors, err := m.Indexer.FindImplementors(ctx, params.InterfaceName, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if len(implementors) == 0 {
		return tools.ToolResult{Text: fmt.Sprintf("No implementors found for %s", params.InterfaceName)}, nil
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Types implementing %s:\n", params.InterfaceName)
	for _, imp := range implementors {
		_, _ = fmt.Fprintf(&sb, "- %s.%s\n", imp.PkgPath, imp.Name)
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (m *defaultTypeManager) FindUsages(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	query := params.Query
	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.SP.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	locs, err := m.Indexer.GetUsages(ctx, query, resolvedPath, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	var results []string
	for _, loc := range locs {
		results = append(results, fmt.Sprintf("%s:%d:%d", loc.Path, loc.Line, loc.Column))
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No usages found."}, nil
	}
	return tools.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *defaultTypeManager) FindDefinitions(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resolvedPath, err := m.resolvePath(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	results, err := m.collectSymbols(ctx, resolvedPath, params.Query, false, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return m.wrapResults(results, "No definitions found."), nil
}

func (m *defaultTypeManager) extractDefinition(ts *ast.TypeSpec, gd *ast.GenDecl, loc location) typeDefinition {
	def := typeDefinition{
		Name:     ts.Name.Name,
		location: fmt.Sprintf("%s:%d", loc.Path, loc.Line),
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

func (m *defaultTypeManager) parseFields(list *ast.FieldList) []fieldInfo {
	if list == nil {
		return nil
	}
	var fields []fieldInfo
	for _, field := range list.List {
		names := []string{}
		for _, n := range field.Names {
			names = append(names, n.Name)
		}
		tag := ""
		if field.Tag != nil {
			tag = field.Tag.Value
		}
		fields = append(fields, fieldInfo{
			Names: strings.Join(names, ", "),
			Type:  exprToString(field.Type),
			Tag:   tag,
		})
	}
	return fields
}

func (m *defaultTypeManager) parseInterfaceMethods(list *ast.FieldList) []string {
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

func (m *defaultTypeManager) findMethodsInPackage(ctx context.Context, dir, typeName string, hb chan<- struct{}) ([]string, error) {
	var methods []string
	count := 0
	walkFn := m.makeMethodWalkFunc(ctx, typeName, hb, &methods, &count)
	err := m.fs.Walk(ctx, dir, walkFn)
	return methods, err
}

func (m *defaultTypeManager) makeMethodWalkFunc(ctx context.Context, typeName string, hb chan<- struct{}, methods *[]string, count *int) persistence.WalkFunc {
	return func(p string, i os.FileInfo, walkErr error) error {
		// Propagate filesystem-level walk errors (permission denied,
		// broken symlink) — these are not parse skips.
		if walkErr != nil {
			return walkErr
		}
		if err := m.checkCancellation(ctx); err != nil {
			return err
		}
		if m.shouldSkipFile(p, i, nil) {
			return nil
		}

		m.handleHeartbeat(hb, count)

		ff, _, err := m.Cache.Get(p)
		if err != nil {
			// Tolerate parse errors in individual files; method
			// discovery is best-effort and a single corrupt .go
			// file must not block the entire walk.
			return nil
		}
		*methods = append(*methods, m.extractMethodsFromFile(ff, typeName)...)
		return nil
	}
}

func (m *defaultTypeManager) extractMethodsFromFile(f *ast.File, typeName string) []string {
	var methods []string
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) > 0 {
			recvType := exprToString(fd.Recv.List[0].Type)
			if strings.TrimPrefix(recvType, "*") == typeName {
				methods = append(methods, getFuncSignature(fd))
			}
		}
	}
	return methods
}

func (m *defaultTypeManager) renderTypeInfo(def typeDefinition, receivers []string) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Type: %s\nLocation: %s\n", def.Name, def.location)
	if def.Kind != "" {
		_, _ = fmt.Fprintf(&sb, "Kind: %s\n", def.Kind)
	}
	if def.Doc != "" {
		sb.WriteString("Doc: " + def.Doc)
	}

	m.renderTypeInfo_fields(def, &sb)

	if len(def.Methods) > 0 {
		sb.WriteString("Methods:\n")
		for _, meth := range def.Methods {
			_, _ = fmt.Fprintf(&sb, "  - %s\n", meth)
		}
	}

	sb.WriteString("Methods (Receivers):\n")
	for _, meth := range receivers {
		_, _ = fmt.Fprintf(&sb, "  - %s\n", meth)
	}

	return sb.String()
}

func (m *defaultTypeManager) renderTypeInfo_fields(def typeDefinition, sb *strings.Builder) {
	if len(def.Fields) > 0 {
		sb.WriteString("Fields:\n")
		for _, f := range def.Fields {
			tag := ""
			if f.Tag != "" {
				tag = " " + f.Tag
			}
			_, _ = fmt.Fprintf(sb, "  - %s %s%s\n", f.Names, f.Type, tag)
		}
	}
}

func (m *defaultTypeManager) classifySymbol(decl ast.Decl) (string, string, bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name, "func", true
	case *ast.GenDecl:
		if len(d.Specs) == 0 {
			return "", "", false
		}
		switch s := d.Specs[0].(type) {
		case *ast.TypeSpec:
			return s.Name.Name, "type", true
		case *ast.ValueSpec:
			if len(s.Names) > 0 {
				kind := "var"
				if d.Tok == token.CONST {
					kind = "const"
				}
				return s.Names[0].Name, kind, true
			}
		}
	}
	return "", "", false
}

func (m *defaultTypeManager) shouldIncludeSymbol(name, query string, exportedOnly bool) bool {
	if exportedOnly && !ast.IsExported(name) {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
		return false
	}
	return true
}

func (m *defaultTypeManager) formatSymbol(name, kind string, decl ast.Decl) string {
	switch kind {
	case "type":
		return "type " + name
	case "func":
		if fd, ok := decl.(*ast.FuncDecl); ok {
			return getFuncSignature(fd)
		}
	case "var":
		return "var " + name
	case "const":
		return "const " + name
	}
	return name
}

func (m *defaultTypeManager) matchSymbolInFile(f *ast.File, fset *token.FileSet, filePath string, query string, exportedOnly bool) []string {
	var results []string
	for _, decl := range f.Decls {
		name, kind, ok := m.classifySymbol(decl)
		if ok && m.shouldIncludeSymbol(name, query, exportedOnly) {
			desc := m.formatSymbol(name, kind, decl)
			pos := fset.Position(decl.Pos())
			results = append(results, fmt.Sprintf("%s:%d: %s", filePath, pos.Line, desc))
		}
	}
	return results
}

func (m *defaultTypeManager) collectSymbols(ctx context.Context, root, query string, exportedOnly bool, hb chan<- struct{}) ([]string, error) {
	var results []string
	count := 0
	walkErr := m.fs.Walk(ctx, root, func(p string, info os.FileInfo, walkErr error) error {
		// Propagate filesystem-level walk errors (permission denied,
		// broken symlink) — these are not parse skips.
		if walkErr != nil {
			return walkErr
		}
		if err := m.checkCancellation(ctx); err != nil {
			return err
		}
		if m.shouldSkipFile(p, info, nil) {
			return nil
		}

		m.handleHeartbeat(hb, &count)

		f, fset, err := m.Cache.Get(p)
		if err != nil {
			// Tolerate parse errors in individual files; symbol
			// collection is best-effort and a single corrupt .go
			// file must not block the entire walk.
			return nil
		}
		results = append(results, m.matchSymbolInFile(f, fset, p, query, exportedOnly)...)
		return nil
	})
	return results, walkErr
}

func (m *defaultTypeManager) resolvePath(path string) (string, error) {
	if path == "" {
		path = "."
	}
	return m.SP.IsPathSafe(path)
}

func (m *defaultTypeManager) wrapResults(results []string, notFoundMsg string) tools.ToolResult {
	if len(results) == 0 {
		return tools.ToolResult{Text: notFoundMsg}
	}
	return tools.ToolResult{Text: strings.Join(results, "\n")}
}

func (m *defaultTypeManager) checkCancellation(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (m *defaultTypeManager) shouldSkipFile(p string, info os.FileInfo, _ error) bool {
	// The error parameter is retained for signature compatibility with
	// filepath.WalkFunc but is always nil at this point — walk errors
	// are intercepted by the caller before shouldSkipFile is invoked.
	return info.IsDir() || filepath.Ext(p) != ".go"
}

func (m *defaultTypeManager) handleHeartbeat(hb chan<- struct{}, count *int) {
	if count == nil {
		return
	}
	*count++
	if *count%10 == 0 && hb != nil {
		select {
		case hb <- struct{}{}:
		default:
		}
	}
}

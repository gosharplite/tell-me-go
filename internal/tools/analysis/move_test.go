package analysis

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoveTransform(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		symbol  string
		srcCode string
		dstCode string
		wantErr bool
	}{
		{
			name:   "move simple function",
			symbol: "Hello",
			srcCode: `package a
func Hello() {}`,
			dstCode: `package b`,
			wantErr: false,
		},
		{
			name:   "move struct",
			symbol: "Config",
			srcCode: `package a
type Config struct {
	Name string
}`,
			dstCode: `package b`,
			wantErr: false,
		},
		{
			name:   "symbol not found",
			symbol: "Missing",
			srcCode: `package a
func Hello() {}`,
			dstCode: `package b`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fset, files, tr, err := setupMoveTransform(tt.symbol, tt.srcCode, tt.dstCode)
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			err = tr.Apply(context.Background(), fset, files)
			if (err != nil) != tt.wantErr {
				t.Errorf("moveTransform.Apply() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				verifyMove(t, tt.symbol, files, tr)
			}
		})
	}
}

func setupMoveTransform(symbol, srcCode, dstCode string) (*token.FileSet, map[string]*ast.File, *moveTransform, error) {
	fset := token.NewFileSet()
	srcFile, err := parser.ParseFile(fset, "src.go", srcCode, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, err
	}
	dstFile, err := parser.ParseFile(fset, "dst.go", dstCode, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, err
	}

	files := map[string]*ast.File{
		"src.go": srcFile,
		"dst.go": dstFile,
	}

	plan := &movePlan{
		Symbol:  symbol,
		SrcFile: "src.go",
		DstFile: "dst.go",
	}
	return fset, files, newMoveTransform(plan), nil
}

func verifyMove(t *testing.T, symbol string, files map[string]*ast.File, tr *moveTransform) {
	// Verify symbol moved
	foundInSrc := false
	for _, d := range files["src.go"].Decls {
		if tr.matchSymbol(d) {
			foundInSrc = true
			break
		}
	}
	if foundInSrc {
		t.Errorf("symbol %s still in source", symbol)
	}

	foundInDst := false
	for _, d := range files["dst.go"].Decls {
		if tr.matchSymbol(d) {
			foundInDst = true
			break
		}
	}
	if !foundInDst {
		t.Errorf("symbol %s not in destination", symbol)
	}
}

func parseTestMoveFile(code string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, parser.ParseComments)
	return fset, f, err
}

func TestMatchSymbol_ValueSpec(t *testing.T) {
	t.Parallel()
	t.Run("matches const", func(t *testing.T) {
		t.Parallel()
		tr := newMoveTransform(&movePlan{Symbol: "MyConst"})
		code := `package a
const MyConst = 42`
		fset, f, err := parseTestMoveFile(code)
		if err != nil {
			t.Fatal(err)
		}
		_ = fset
		if !tr.matchSymbol(f.Decls[0]) {
			t.Error("matchSymbol should match const declaration")
		}
	})
	t.Run("matches var", func(t *testing.T) {
		t.Parallel()
		tr := newMoveTransform(&movePlan{Symbol: "MyVar"})
		code := `package a
var MyVar = "hello"`
		fset, f, err := parseTestMoveFile(code)
		if err != nil {
			t.Fatal(err)
		}
		_ = fset
		if !tr.matchSymbol(f.Decls[0]) {
			t.Error("matchSymbol should match var declaration")
		}
	})
}

func TestMovePlanDescription(t *testing.T) {
	t.Parallel()
	plan := &movePlan{Symbol: "Foo", SrcFile: "a.go", DstFile: "b.go"}
	desc := plan.Description()
	if desc != "Move Foo from a.go to b.go" {
		t.Errorf("unexpected description: %q", desc)
	}
}

func TestMatchesTypeName_StarExpr_PointerReceiver(t *testing.T) {
	t.Parallel()

	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	fset, f, err := parseTestMoveFile("package a\ntype MyStruct struct{}\nfunc (s *MyStruct) Method() {}")
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	// Find the method decl
	var methodDecl *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
			methodDecl = fd
			break
		}
	}
	if methodDecl == nil {
		t.Fatal("expected to find method declaration")
	}
	// matchesTypeName on *MyStruct receiver
	if !tr.matchesTypeName(methodDecl.Recv.List[0].Type, "MyStruct") {
		t.Error("matchesTypeName should match *MyStruct pointer receiver")
	}
}

func TestMatchesTypeName_StarExpr_NonMatch(t *testing.T) {
	t.Parallel()

	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	fset, f, err := parseTestMoveFile("package a\ntype Other struct{}\nfunc (s *Other) Method() {}")
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	var methodDecl *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
			methodDecl = fd
			break
		}
	}
	if methodDecl == nil {
		t.Fatal("expected to find method declaration")
	}
	if tr.matchesTypeName(methodDecl.Recv.List[0].Type, "MyStruct") {
		t.Error("matchesTypeName should NOT match *Other as MyStruct")
	}
}

func TestMatchesTypeName_StarExpr_DoublePointer(t *testing.T) {
	t.Parallel()

	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	// **MyStruct - StarExpr wrapping another StarExpr wrapping Ident
	// matchesTypeName recurses through StarExpr, so this matches.
	innerStar := &ast.StarExpr{X: &ast.Ident{Name: "MyStruct"}}
	doubleStar := &ast.StarExpr{X: innerStar}
	if !tr.matchesTypeName(doubleStar, "MyStruct") {
		t.Error("matchesTypeName should match **MyStruct via recursion through StarExpr")
	}
}

func TestMatchesTypeName_StarExpr_UnhandledExpr(t *testing.T) {
	t.Parallel()

	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	// ArrayType is not handled by the switch
	if tr.matchesTypeName(&ast.ArrayType{}, "MyStruct") {
		t.Error("matchesTypeName should return false for unhandled expr type")
	}
}

// TestMatchFuncDecl verifies the matchFuncDecl helper correctly identifies
// FuncDecl nodes by name and rejects non-FuncDecl or BadDecl inputs.
func TestMatchFuncDecl(t *testing.T) {
	t.Parallel()

	t.Run("matches by name", func(t *testing.T) {
		t.Parallel()
		decl := &ast.FuncDecl{Name: &ast.Ident{Name: "Hello"}}
		if !matchFuncDecl(decl, "Hello") {
			t.Error("matchFuncDecl should match FuncDecl by name")
		}
	})

	t.Run("non-FuncDecl", func(t *testing.T) {
		t.Parallel()
		if matchFuncDecl(&ast.BadDecl{}, "Hello") {
			t.Error("matchFuncDecl should return false for BadDecl")
		}
	})

	t.Run("BadDecl", func(t *testing.T) {
		t.Parallel()
		if matchFuncDecl(&ast.BadDecl{}, "Nope") {
			t.Error("matchFuncDecl should return false for BadDecl")
		}
	})
}

// TestMatchTypeSpec verifies the matchTypeSpec helper identifies TypeSpec
// nodes within GenDecl and rejects non-GenDecl inputs like FuncDecl.
func TestMatchTypeSpec(t *testing.T) {
	t.Parallel()

	t.Run("matches type by name", func(t *testing.T) {
		t.Parallel()
		decl := &ast.GenDecl{
			Specs: []ast.Spec{
				&ast.TypeSpec{Name: &ast.Ident{Name: "MyType"}},
			},
		}
		if !matchTypeSpec(decl, "MyType") {
			t.Error("matchTypeSpec should match TypeSpec by name")
		}
	})

	t.Run("returns false for FuncDecl", func(t *testing.T) {
		t.Parallel()
		decl := &ast.FuncDecl{Name: &ast.Ident{Name: "MyType"}}
		if matchTypeSpec(decl, "MyType") {
			t.Error("matchTypeSpec should return false for FuncDecl")
		}
	})
}

// TestMatchValueSpec verifies the matchValueSpec helper identifies
// ValueSpec nodes (const, var, multi-name) and rejects ImportSpec
// or non-GenDecl inputs.
func TestMatchValueSpec(t *testing.T) {
	t.Parallel()

	t.Run("matches const by name", func(t *testing.T) {
		t.Parallel()
		decl := &ast.GenDecl{
			Specs: []ast.Spec{
				&ast.ValueSpec{Names: []*ast.Ident{{Name: "MyConst"}}},
			},
		}
		if !matchValueSpec(decl, "MyConst") {
			t.Error("matchValueSpec should match const by name")
		}
	})

	t.Run("matches var by name", func(t *testing.T) {
		t.Parallel()
		decl := &ast.GenDecl{
			Specs: []ast.Spec{
				&ast.ValueSpec{Names: []*ast.Ident{{Name: "MyVar"}}},
			},
		}
		if !matchValueSpec(decl, "MyVar") {
			t.Error("matchValueSpec should match var by name")
		}
	})

	t.Run("matches second name in multi-name ValueSpec", func(t *testing.T) {
		t.Parallel()
		decl := &ast.GenDecl{
			Specs: []ast.Spec{
				&ast.ValueSpec{Names: []*ast.Ident{{Name: "A"}, {Name: "B"}}},
			},
		}
		if !matchValueSpec(decl, "B") {
			t.Error("matchValueSpec should match second name in multi-name ValueSpec")
		}
	})

	t.Run("returns false for import GenDecl", func(t *testing.T) {
		t.Parallel()
		decl := &ast.GenDecl{
			Specs: []ast.Spec{
				&ast.ImportSpec{Name: &ast.Ident{Name: "fmt"}},
			},
		}
		if matchValueSpec(decl, "fmt") {
			t.Error("matchValueSpec should return false for ImportSpec (not ValueSpec)")
		}
	})

	t.Run("returns false for FuncDecl", func(t *testing.T) {
		t.Parallel()
		decl := &ast.FuncDecl{Name: &ast.Ident{Name: "MyFunc"}}
		if matchValueSpec(decl, "MyFunc") {
			t.Error("matchValueSpec should return false for FuncDecl")
		}
	})
}

func TestMatchSymbol_GenDecl_ImportReturnsFalse(t *testing.T) {
	t.Parallel()
	// GenDecl with an ImportSpec — not a TypeSpec or ValueSpec
	tr := newMoveTransform(&movePlan{Symbol: "fmt"})
	code := `package a
import "fmt"`
	fset, f, err := parseTestMoveFile(code)
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	// GenDecl with Tok==token.IMPORT and Specs containing ImportSpec
	if tr.matchSymbol(f.Decls[0]) {
		t.Error("matchSymbol should return false for import GenDecl (not TypeSpec or ValueSpec)")
	}
}

func TestMatchSymbol_GenDecl_MultiValueSpec(t *testing.T) {
	t.Parallel()
	tr := newMoveTransform(&movePlan{Symbol: "B"})
	code := `package a
var A, B = 1, 2`
	fset, f, err := parseTestMoveFile(code)
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	if !tr.matchSymbol(f.Decls[0]) {
		t.Error("matchSymbol should match B in multi-name ValueSpec")
	}
}

func TestMatchSymbol_IsMethodOf_ValueReceiver(t *testing.T) {
	t.Parallel()
	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	fset, f, err := parseTestMoveFile("package a\ntype MyStruct struct{}\nfunc (s MyStruct) ValueMethod() {}")
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	// Find the method decl
	var methodDecl ast.Decl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
			methodDecl = d
			break
		}
	}
	if !tr.isMethodOf(methodDecl, "MyStruct") {
		t.Error("isMethodOf should match value receiver method of MyStruct")
	}
}

func TestMatchSymbol_IsMethodOf_NonFuncDecl(t *testing.T) {
	t.Parallel()
	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	fset, f, err := parseTestMoveFile("package a\ntype MyStruct struct{}")
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	if tr.isMethodOf(f.Decls[0], "MyStruct") {
		t.Error("isMethodOf should return false for GenDecl (not FuncDecl)")
	}
}

func TestMatchSymbol_IsMethodOf_NilReceiver(t *testing.T) {
	t.Parallel()
	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})
	fset, f, err := parseTestMoveFile("package a\nfunc PlainFunc() {}")
	if err != nil {
		t.Fatal(err)
	}
	_ = fset
	if tr.isMethodOf(f.Decls[0], "MyStruct") {
		t.Error("isMethodOf should return false for function without receiver")
	}
}

func TestMatchSymbol_BadDeclReturnsFalse(t *testing.T) {
	t.Parallel()
	tr := newMoveTransform(&movePlan{Symbol: "Anything"})
	// BadDecl is not *ast.FuncDecl or *ast.GenDecl
	if tr.matchSymbol(&ast.BadDecl{}) {
		t.Error("matchSymbol should return false for BadDecl")
	}
}

func TestMoveTransform_ErrorContext(t *testing.T) {
	t.Parallel()

	t.Run("source_not_loaded_includes_symbol", func(t *testing.T) {
		t.Parallel()
		plan := &movePlan{Symbol: "MyFunc", SrcFile: "missing.go", DstFile: "dst.go"}
		tr := newMoveTransform(plan)

		fset := token.NewFileSet()
		files := map[string]*ast.File{
			"dst.go": {},
		}

		err := tr.Apply(context.Background(), fset, files)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move MyFunc")
		assert.Contains(t, err.Error(), "source file missing.go not loaded")
	})

	t.Run("dest_not_loaded_includes_symbol", func(t *testing.T) {
		t.Parallel()
		plan := &movePlan{Symbol: "MyFunc", SrcFile: "src.go", DstFile: "missing.go"}
		tr := newMoveTransform(plan)

		fset, srcFile, err := parseTestMoveFile("package a\nfunc MyFunc() {}")
		require.NoError(t, err)
		_ = fset
		files := map[string]*ast.File{
			"src.go": srcFile,
		}

		err = tr.Apply(context.Background(), token.NewFileSet(), files)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move MyFunc")
		assert.Contains(t, err.Error(), "destination file missing.go not loaded")
	})

	t.Run("symbol_not_found_includes_symbol", func(t *testing.T) {
		t.Parallel()
		plan := &movePlan{Symbol: "NonExistent", SrcFile: "src.go", DstFile: "dst.go"}
		tr := newMoveTransform(plan)

		fset, srcFile, err := parseTestMoveFile("package a\nfunc Hello() {}")
		require.NoError(t, err)
		_ = fset
		_, dstFile, _ := parseTestMoveFile("package a")
		files := map[string]*ast.File{
			"src.go": srcFile,
			"dst.go": dstFile,
		}

		err = tr.Apply(context.Background(), token.NewFileSet(), files)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move NonExistent")
		assert.Contains(t, err.Error(), "symbol not found")
	})
}

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

func TestMatchesTypeName_StarExpr(t *testing.T) {
	t.Parallel()

	tr := newMoveTransform(&movePlan{Symbol: "MyStruct"})

	t.Run("pointer receiver matches", func(t *testing.T) {
		t.Parallel()
		// *ast.StarExpr{X: *ast.Ident{Name: "MyStruct"}}
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
	})

	t.Run("pointer receiver non-match", func(t *testing.T) {
		t.Parallel()
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
	})

	t.Run("double pointer handled by recursion", func(t *testing.T) {
		t.Parallel()
		// **MyStruct - StarExpr wrapping another StarExpr wrapping Ident
		// matchesTypeName recurses through StarExpr, so this matches.
		innerStar := &ast.StarExpr{X: &ast.Ident{Name: "MyStruct"}}
		doubleStar := &ast.StarExpr{X: innerStar}
		if !tr.matchesTypeName(doubleStar, "MyStruct") {
			t.Error("matchesTypeName should match **MyStruct via recursion through StarExpr")
		}
	})

	t.Run("unhandled expr type returns false", func(t *testing.T) {
		t.Parallel()
		// ArrayType is not handled by the switch
		if tr.matchesTypeName(&ast.ArrayType{}, "MyStruct") {
			t.Error("matchesTypeName should return false for unhandled expr type")
		}
	})
}

func TestMatchSymbol_GenDeclEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("non-type non-value GenDecl returns false", func(t *testing.T) {
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
	})

	t.Run("multiple ValueSpecs matches second", func(t *testing.T) {
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
	})

	t.Run("isMethodOf with value receiver", func(t *testing.T) {
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
	})

	t.Run("isMethodOf non-FuncDecl returns false", func(t *testing.T) {
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
	})

	t.Run("isMethodOf nil receiver returns false", func(t *testing.T) {
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
	})

	t.Run("matchSymbol non-FuncDecl non-GenDecl returns false", func(t *testing.T) {
		t.Parallel()
		tr := newMoveTransform(&movePlan{Symbol: "Anything"})
		// BadDecl is not *ast.FuncDecl or *ast.GenDecl
		if tr.matchSymbol(&ast.BadDecl{}) {
			t.Error("matchSymbol should return false for BadDecl")
		}
	})
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

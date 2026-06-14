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

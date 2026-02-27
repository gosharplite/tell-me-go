package analysis

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
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

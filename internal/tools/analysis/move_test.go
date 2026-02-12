package analysis

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestMoveTransform(t *testing.T) {
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
			fset := token.NewFileSet()
			srcFile, err := parser.ParseFile(fset, "src.go", tt.srcCode, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse src: %v", err)
			}
			dstFile, err := parser.ParseFile(fset, "dst.go", tt.dstCode, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse dst: %v", err)
			}

			files := map[string]*ast.File{
				"src.go": srcFile,
				"dst.go": dstFile,
			}

			plan := &MovePlan{
				Symbol:  tt.symbol,
				SrcFile: "src.go",
				DstFile: "dst.go",
			}
			tr := newMoveTransform(plan)

			err = tr.Apply(context.Background(), fset, files)
			if (err != nil) != tt.wantErr {
				t.Errorf("moveTransform.Apply() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify symbol moved
				foundInSrc := false
				for _, d := range files["src.go"].Decls {
					if tr.matchSymbol(d) {
						foundInSrc = true
						break
					}
				}
				if foundInSrc {
					t.Errorf("symbol %s still in source", tt.symbol)
				}

				foundInDst := false
				for _, d := range files["dst.go"].Decls {
					if tr.matchSymbol(d) {
						foundInDst = true
						break
					}
				}
				if !foundInDst {
					t.Errorf("symbol %s not in destination", tt.symbol)
				}
			}
		})
	}
}

package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestCalculateComplexity(t *testing.T) {
	tests := []struct {
		name string
		code string
		want int
	}{
		{
			name: "simple",
			code: `package p
func f() {}`,
			want: 1,
		},
		{
			name: "if-else",
			code: `package p
func f(a bool) {
	if a {
	} else {
	}
}`,
			want: 2,
		},
		{
			name: "for-loop",
			code: `package p
func f() {
	for i := 0; i < 10; i++ {
	}
}`,
			want: 2,
		},
		{
			name: "switch-case",
			code: `package p
func f(i int) {
	switch i {
	case 1:
	case 2:
	default:
	}
}`,
			want: 4, // 1 (base) + 3 (case 1, case 2, default)
		},
		{
			name: "logical-operators",
			code: `package p
func f(a, b bool) {
	if a && b || a {
	}
}`,
			want: 4, // 1 (base) + 1 (if) + 1 (&&) + 1 (||)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatal(err)
			}
			fd := f.Decls[0].(*ast.FuncDecl)
			if got := CalculateComplexity(fd); got != tt.want {
				t.Errorf("CalculateComplexity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetFuncSignature(t *testing.T) {
	code := `package p
func (s *S) Foo(a int, b string) (int, error) { return 0, nil }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}
	fd := f.Decls[0].(*ast.FuncDecl)
	got := GetFuncSignature(fd)
	want := "func (s *S) Foo(a int, b string) (int, error)"
	if got != want {
		t.Errorf("GetFuncSignature() = %v, want %v", got, want)
	}
}

package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
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

func TestFindTypeSpec(t *testing.T) {
	code := `package p
type S struct { A int }
type (
	A struct{}
	B interface{}
)
type Alias = string
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		typename string
		found    bool
	}{
		{"struct", "S", true},
		{"nested struct", "A", true},
		{"interface", "B", true},
		{"alias", "Alias", true},
		{"missing", "C", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, gd := FindTypeSpec(f, tt.typename)
			if tt.found {
				if ts == nil || gd == nil {
					t.Errorf("FindTypeSpec() expected to find %s", tt.typename)
				} else if ts.Name.Name != tt.typename {
					t.Errorf("FindTypeSpec() found %s, want %s", ts.Name.Name, tt.typename)
				}
			} else {
				if ts != nil || gd != nil {
					t.Errorf("FindTypeSpec() expected not to find %s", tt.typename)
				}
			}
		})
	}
}

func TestExprToString(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{"ident", "int", "int"},
		{"star", "*int", "*int"},
		{"selector", "fmt.Stringer", "fmt.Stringer"},
		{"array", "[]int", "[]int"},
		{"map", "map[string]int", "map[string]int"},
		{"interface", "interface{}", "interface{}"},
		{"func", "func(int) string", "func(...)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.code)
			if err != nil {
				t.Fatal(err)
			}
			if got := ExprToString(expr); got != tt.want {
				t.Errorf("ExprToString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestASTCache(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.go")
	os.WriteFile(path, []byte("package p\n"), 0644)

	cache := NewASTCache()
	f1, _, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	if f1 == nil {
		t.Fatal("expected file, got nil")
	}

	// Test cache hit
	f2, _, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	if f1 != f2 {
		t.Error("expected same file pointer from cache")
	}

	// Test eviction
	cache.maxSize = 1
	path2 := filepath.Join(tmpDir, "test2.go")
	os.WriteFile(path2, []byte("package p2\n"), 0644)
	cache.Get(path2)

	if len(cache.files) > 1 {
		t.Errorf("expected cache size 1, got %d", len(cache.files))
	}
}

func TestGetFileSkeletonGo(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.go")
	code := `package p
// F is a function
func F() {}
type S struct{}
type I interface{}
`
	os.WriteFile(path, []byte(code), 0644)
	cache := NewASTCache()
	got, err := cache.GetFileSkeletonGo(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func F()", "type S struct", "type I interface"} {
		if !strings.Contains(got, want) {
			t.Errorf("skeleton missing %q", want)
		}
	}
}

func TestCompareASTs(t *testing.T) {
	fset := token.NewFileSet()
	baseCode := `package p
func F1() {}
func F2() {}
type T1 struct{}
`
	currCode := `package p
func F1() { println(1) }
func F3() {}
type T1 struct { A int }
`
	base, _ := parser.ParseFile(fset, "base.go", baseCode, parser.ParseComments)
	curr, _ := parser.ParseFile(fset, "curr.go", currCode, parser.ParseComments)

	changes := CompareASTs(base, curr)
	
	expected := []string{"Modified: func F1", "Added: func F3", "Modified: type T1", "Deleted: func F2"}
	for _, exp := range expected {
		found := false
		for _, c := range changes {
			if strings.Contains(c, exp) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected change %q not found in %v", exp, changes)
		}
	}
}

func TestGetFuncTypeSig(t *testing.T) {
	code := `package p
type T func(a int, b string) (int, error)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}
	ts := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
	ft := ts.Type.(*ast.FuncType)
	got := GetFuncTypeSig(ft)
	want := "(int, string) (int, error)"
	if got != want {
		t.Errorf("GetFuncTypeSig() = %v, want %v", got, want)
	}
}

func TestGetDeclKey_ConstVar(t *testing.T) {
	code := `package p
const C = 1
var V = 2
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}
	
	k1 := GetDeclKey(f.Decls[0])
	if k1 != "const block" {
		t.Errorf("expected const block, got %s", k1)
	}
	
	k2 := GetDeclKey(f.Decls[1])
	if k2 != "var block" {
		t.Errorf("expected var block, got %s", k2)
	}
}

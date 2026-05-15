// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExprToString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		expr ast.Expr
		want string
	}{
		{"ident", &ast.Ident{Name: "int"}, "int"},
		{"selector", &ast.SelectorExpr{X: &ast.Ident{Name: "pkg"}, Sel: &ast.Ident{Name: "Type"}}, "pkg.Type"},
		{"star", &ast.StarExpr{X: &ast.Ident{Name: "int"}}, "*int"},
		{"array", &ast.ArrayType{Elt: &ast.Ident{Name: "int"}}, "[]int"},
		{"map", &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.Ident{Name: "int"}}, "map[string]int"},
		{"interface", &ast.InterfaceType{}, "interface{}"},
		{"ellipsis", &ast.Ellipsis{Elt: &ast.Ident{Name: "int"}}, "...int"},
		{"func", &ast.FuncType{}, "func(...)"},
		{"default", &ast.BasicLit{Kind: token.INT, Value: "1"}, "*ast.BasicLit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exprToString(tt.expr); got != tt.want {
				t.Errorf("exprToString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetFuncSignature(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	code := `package main
func F1() {}
func (r R) F2(a int) bool { return true }
func (r *R) F3(a, b int, c string) (int, error) { return 0, nil }
func F4(a ...int) {}
`
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}

	signatures := make(map[string]string)
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			signatures[fd.Name.Name] = getFuncSignature(fd)
		}
	}

	tests := []struct {
		name string
		want string
	}{
		{"F1", "func F1()"},
		{"F2", "func (r R) F2(a int) bool"},
		{"F3", "func (r *R) F3(a, b int, c string) (int, error)"},
		{"F4", "func F4(a ...int)"},
	}

	for _, tt := range tests {
		if got := signatures[tt.name]; got != tt.want {
			t.Errorf("getFuncSignature(%s) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCalculateComplexity(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	code := `package main
func C1() {}
func C2(a bool) {
	if a {
		return
	}
}
func C3(a, b bool) {
	if a && b || a {
		for i := 0; i < 10; i++ {
			switch i {
			case 1:
			default:
			}
		}
	}
}
func C4(ch chan int) {
	select {
	case <-ch:
	default:
	}
	for range ch {}
}
`
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	if err != nil {
		t.Fatal(err)
	}

	complexities := make(map[string]int)
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			complexities[fd.Name.Name] = calculateComplexity(fd)
		}
	}

	tests := []struct {
		name string
		want int
	}{
		{"C1", 1},
		{"C2", 2},
		{"C3", 7}, // 1 (base) + 1 (if) + 2 (&&, ||) + 1 (for) + 2 (case, default) = 7
		{"C4", 4}, // 1 (base) + 1 (case) + 1 (default) + 1 (range) = 4
	}

	for _, tt := range tests {
		if got := complexities[tt.name]; got != tt.want {
			t.Errorf("calculateComplexity(%s) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestCompareASTs(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	baseCode := `package p
type T int
func F() {}
var V = 1
const C = 2
`
	currCode := `package p
type T string
func G() {}
var V = 1
const C = 3
`
	base, _ := parser.ParseFile(fset, "base.go", baseCode, 0)
	curr, _ := parser.ParseFile(fset, "curr.go", currCode, 0)

	changes := compareASTs(base, curr)
	changeMap := make(map[string]bool)
	for _, c := range changes {
		changeMap[c] = true
	}

	expected := []string{
		"Modified: type T",
		"Added: func G",
		"Deleted: func F",
		"Modified: const block",
	}

	for _, e := range expected {
		if !changeMap[e] {
			t.Errorf("expected change %q not found", e)
		}
	}
}

func TestASTCache(t *testing.T) {
	t.Parallel()

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		content := "package main\nfunc main() {}\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		testASTCacheGet(t, path)
	})
	t.Run("Hit", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		content := "package main\nfunc main() {}\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		testASTCacheHit(t, path)
	})
	t.Run("Invalidation", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.go")
		content := "package main\nfunc main() {}\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		testASTCacheInvalidation(t, path)
	})
	t.Run("NonExistent", func(t *testing.T) {
		t.Parallel()
		testASTCacheNonExistent(t)
	})
	t.Run("SyntaxError", func(t *testing.T) {
		t.Parallel()
		testASTCacheSyntaxError(t, t.TempDir())
	})
	t.Run("Eviction", func(t *testing.T) {
		t.Parallel()
		testASTCacheEviction(t, t.TempDir())
	})
}

func testASTCacheGet(t *testing.T, path string) {
	cache := newASTCache(".")
	f, _, err := cache.Get(path)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if f.Name.Name != "main" {
		t.Errorf("expected package main, got %s", f.Name.Name)
	}
}

func testASTCacheHit(t *testing.T, path string) {
	cache := newASTCache(".")
	f1, fset1, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	f2, fset2, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	if f1 != f2 || fset1 != fset2 {
		t.Error("expected cache hit to return same objects")
	}
}

func testASTCacheInvalidation(t *testing.T, path string) {
	cache := newASTCache(".")

	// Set initial time
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, t1, t1); err != nil {
		t.Fatal(err)
	}

	f1, _, err := cache.Get(path)
	if err != nil {
		t.Fatalf("first Get failed: %v", err)
	}

	// Set DIFFERENT time and update content
	t2 := time.Date(2020, 1, 1, 0, 0, 1, 0, time.UTC)
	if err := os.WriteFile(path, []byte("package main\nfunc main() { _ = 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, t2, t2); err != nil {
		t.Fatal(err)
	}

	f2, _, err := cache.Get(path)
	if err != nil {
		t.Fatalf("second Get failed: %v", err)
	}
	if f1 == f2 {
		t.Error("expected cache invalidation after file update")
	}
}

func testASTCacheNonExistent(t *testing.T) {
	cache := newASTCache(".")
	_, _, err := cache.Get("non_existent.go")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func testASTCacheSyntaxError(t *testing.T, tmpDir string) {
	cache := newASTCache(".")
	invalidPath := filepath.Join(tmpDir, "invalid.go")
	if err := os.WriteFile(invalidPath, []byte("package"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err := cache.Get(invalidPath)
	if err == nil {
		t.Error("expected error for invalid syntax")
	}
}

func testASTCacheEviction(t *testing.T, tmpDir string) {
	cache := newASTCache(".")
	cache.maxSize = 2

	files := []string{
		filepath.Join(tmpDir, "f1.go"),
		filepath.Join(tmpDir, "f2.go"),
		filepath.Join(tmpDir, "f3.go"),
	}

	for _, f := range files {
		if err := os.WriteFile(f, []byte("package p"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := cache.Get(f); err != nil {
			t.Fatal(err)
		}
	}

	if len(cache.files) > 2 {
		t.Errorf("expected cache size <= 2, got %d", len(cache.files))
	}
}

func TestFindTypeSpec(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	code := `package p
type T1 int
type T2 struct{}
`
	f, _ := parser.ParseFile(fset, "t.go", code, 0)

	ts, _ := findTypeSpec(f, "T1")
	if ts == nil || ts.Name.Name != "T1" {
		t.Error("failed to find T1")
	}

	ts, _ = findTypeSpec(f, "NonExistent")
	if ts != nil {
		t.Error("should not have found NonExistent")
	}
}

func TestGetFileSkeletonGo(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "skeleton.go")
	code := `package p
type Exported int
type unexported string
func ExportedFunc() {}
func unexportedFunc() {}
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	skeleton, err := cache.GetFileSkeletonGo(path)
	if err != nil {
		t.Fatalf("GetFileSkeletonGo failed: %v", err)
	}

	if !strings.Contains(skeleton, "type Exported int") {
		t.Error("missing Exported type")
	}
	if strings.Contains(skeleton, "unexported") {
		t.Error("should not contain unexported members")
	}
	if !strings.Contains(skeleton, "func ExportedFunc()") {
		t.Error("missing ExportedFunc")
	}
}

func TestGetDeclKey_Unknown(t *testing.T) {
	t.Parallel()
	if got := getDeclKey(&ast.BadDecl{}); got != "unknown" {
		t.Errorf("getDeclKey(BadDecl) = %s, want unknown", got)
	}
}

func TestGetCachedLineCount(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "lines.go")
	content := "package main\n\nfunc main() {\n\t// comment\n}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")

	// 1. Set specific time
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, t1, t1); err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(path)

	// Initially not in cache
	count, ok := cache.GetCachedLineCount(path, info1)
	if ok {
		t.Errorf("expected not in cache, but got count %d", count)
	}

	// 2. Parse and cache
	_, _, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Now it should be in cache
	count, ok = cache.GetCachedLineCount(path, info1)
	if !ok {
		t.Error("expected to be in cache")
	}
	if count != 5 {
		t.Errorf("expected 5 lines, got %d", count)
	}

	// 4. Invalidation check
	t2 := time.Date(2020, 1, 1, 0, 0, 1, 0, time.UTC)
	if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, t2, t2); err != nil {
		t.Fatal(err)
	}
	newInfo, _ := os.Stat(path)
	_, ok = cache.GetCachedLineCount(path, newInfo)
	if ok {
		t.Error("expected cache invalidation")
	}
}

func TestASTCache_DeterministicEviction(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Repeat 100 times to ensure determinism
	for i := 0; i < 100; i++ {
		cache := newASTCache(".")
		cache.maxSize = 2

		f1 := filepath.Join(tmpDir, "f1.go")
		f2 := filepath.Join(tmpDir, "f2.go")
		f3 := filepath.Join(tmpDir, "f3.go")

		files := []string{f1, f2, f3}
		for _, f := range files {
			if err := os.WriteFile(f, []byte("package p"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := cache.Get(f); err != nil {
				t.Fatal(err)
			}
		}

		// In FIFO, f1 should be evicted first.
		cache.mu.RLock()
		_, ok1 := cache.files[f1]
		_, ok2 := cache.files[f2]
		_, ok3 := cache.files[f3]
		cache.mu.RUnlock()

		if ok1 {
			t.Errorf("Iteration %d: expected f1 to be evicted", i)
		}
		if !ok2 {
			t.Errorf("Iteration %d: expected f2 to be in cache", i)
		}
		if !ok3 {
			t.Errorf("Iteration %d: expected f3 to be in cache", i)
		}
	}
}

func TestHandleFuncDeclKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "bare function",
			code: "package p; func F() {}",
			want: "func F",
		},
		{
			name: "pointer receiver",
			code: "package p; type S struct{}; func (s *S) M() {}",
			want: "func (*S) M",
		},
		{
			name: "value receiver",
			code: "package p; type S struct{}; func (s S) M() {}",
			want: "func (S) M",
		},
		{
			name: "no receiver",
			code: "package p; func NoRecv() {}",
			want: "func NoRecv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					got := handleFuncDeclKey(fd)
					if got != tt.want {
						t.Errorf("handleFuncDeclKey() = %q, want %q", got, tt.want)
					}
				}
			}
		})
	}
}

func TestHandleGenDeclKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "type spec",
			code: "package p; type T int",
			want: "type T",
		},
		{
			name: "const block",
			code: "package p; const ( C = 1 )",
			want: "const block",
		},
		{
			name: "var block",
			code: "package p; var ( V = 1 )",
			want: "var block",
		},
		{
			name: "type with multiple specs",
			code: "package p; type ( T1 int; T2 string )",
			want: "type T1",
		},
		{
			name: "import block returns unknown",
			code: "package p; import ( \"fmt\" )",
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range f.Decls {
				if gd, ok := decl.(*ast.GenDecl); ok {
					got := handleGenDeclKey(gd)
					if got != tt.want {
						t.Errorf("handleGenDeclKey() = %q, want %q", got, tt.want)
					}
				}
			}
		})
	}
}

func TestIsDeclEqual(t *testing.T) {
	t.Parallel()

	t.Run("identical decls", func(t *testing.T) {
		t.Parallel()
		fset := token.NewFileSet()
		code := "package p; type T int"
		f1, _ := parser.ParseFile(fset, "a.go", code, 0)
		f2, _ := parser.ParseFile(fset, "b.go", code, 0)
		equal, err := isDeclEqual(f1.Decls[0], f2.Decls[0])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !equal {
			t.Error("expected identical decls to be equal")
		}
	})

	t.Run("different decls", func(t *testing.T) {
		t.Parallel()
		fset := token.NewFileSet()
		f1, _ := parser.ParseFile(fset, "a.go", "package p; type T int", 0)
		f2, _ := parser.ParseFile(fset, "b.go", "package p; type T string", 0)
		equal, err := isDeclEqual(f1.Decls[0], f2.Decls[0])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if equal {
			t.Error("expected different decls to not be equal")
		}
	})

	t.Run("malformed decl returns false", func(t *testing.T) {
		t.Parallel()
		// Two different empty-node decls: BadDecl produces empty output from format.Node,
		// so comparing two BadDecls returns true. Test with different AST decls instead.
		fset := token.NewFileSet()
		f1, _ := parser.ParseFile(fset, "a.go", "package p; type T int", 0)
		// Intentionally mismatched: GenDecl vs FuncDecl
		f2, _ := parser.ParseFile(fset, "b.go", "package p; func F() {}", 0)
		equal, err := isDeclEqual(f1.Decls[0], f2.Decls[0])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if equal {
			t.Error("expected different decl types to not be equal")
		}
	})

	t.Run("format error returns error", func(t *testing.T) {
		t.Parallel()
		// nil ast.Decl causes format.Node to return "unsupported node type <nil>"
		equal, err := isDeclEqual(nil, nil)
		if err == nil {
			t.Fatal("expected error for nil ast.Decl, got nil")
		}
		if equal {
			t.Error("expected false when format error occurs")
		}
		if !strings.Contains(err.Error(), "format") {
			t.Errorf("expected error to contain 'format', got: %v", err)
		}
	})
}

func TestGetValidEntry_NonExistent(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	entry, ok := cache.getValidEntry("/nonexistent/path/that/really/does/not/exist.go")
	if ok {
		t.Error("expected false for non-existent path")
	}
	if entry.file != nil || entry.fset != nil {
		t.Error("expected zero-value cachedFile for non-existent path")
	}
}

func TestGetFileSkeletonGo_ErrorPath(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	_, err := cache.GetFileSkeletonGo("/nonexistent/path/file.go")
	if err == nil {
		t.Error("expected error for non-existent file path")
	}
}

func TestGetFileSkeletonGo_UnexportedFiltering(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "mixed.go")
	code := `package test
type ExportedType struct {
	ExportedField   int
	unexportedField string
}
func ExportedFunc(a int) string { return "" }
func unexportedFunc()           {}

type unexportedType int

var ExportedVar = 1
var unexportedVar = 2

const ExportedConst = 1
const unexportedConst = 2
`
	if err := os.WriteFile(path, []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	cache := newASTCache(".")
	skeleton, err := cache.GetFileSkeletonGo(path)
	if err != nil {
		t.Fatalf("GetFileSkeletonGo failed: %v", err)
	}

	// Should have exported type with all its fields
	if !strings.Contains(skeleton, "type ExportedType struct") {
		t.Error("missing ExportedType")
	}
	// Should have exported function
	if !strings.Contains(skeleton, "func ExportedFunc(") {
		t.Error("missing ExportedFunc")
	}
	// Should NOT have unexported type name
	if strings.Contains(skeleton, "unexportedType") {
		t.Errorf("skeleton should not contain unexportedType, got:\n%s", skeleton)
	}
	// Should NOT have unexported function
	if strings.Contains(skeleton, "func unexportedFunc") {
		t.Errorf("skeleton should not contain unexportedFunc, got:\n%s", skeleton)
	}
	// writeGenDeclSkeleton only writes TYPE GenDecls, not VAR or CONST
}

func TestASTCache_AbsPath(t *testing.T) {
	t.Parallel()

	t.Run("absolute path passes through", func(t *testing.T) {
		t.Parallel()
		cache := newASTCache("/base")
		abs := cache.absPath("/absolute/path/file.go")
		if abs != "/absolute/path/file.go" {
			t.Errorf("expected /absolute/path/file.go, got %s", abs)
		}
	})

	t.Run("relative path joins base", func(t *testing.T) {
		t.Parallel()
		cache := newASTCache("/base")
		abs := cache.absPath("relative/file.go")
		if abs != filepath.Join("/base", "relative/file.go") {
			t.Errorf("expected joined path, got %s", abs)
		}
	})

	t.Run("empty base dir", func(t *testing.T) {
		t.Parallel()
		cache := newASTCache("")
		abs := cache.absPath("relative/file.go")
		if abs != "relative/file.go" {
			t.Errorf("expected relative/file.go, got %s", abs)
		}
	})
}

func TestWriteGenDeclSkeleton_NonTypeGenDecl(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	// writeGenDeclSkeleton should skip non-TYPE GenDecls
	var sb strings.Builder
	gd := &ast.GenDecl{Tok: token.CONST}
	cache.writeGenDeclSkeleton(&sb, token.NewFileSet(), gd)
	if sb.Len() != 0 {
		t.Errorf("expected empty output for CONST GenDecl, got %q", sb.String())
	}
}

func TestWriteFuncDeclSkeleton_Unexported(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	var sb strings.Builder
	fd := &ast.FuncDecl{
		Name: &ast.Ident{Name: "unexported"},
		Type: &ast.FuncType{Params: &ast.FieldList{}},
	}
	cache.writeFuncDeclSkeleton(&sb, token.NewFileSet(), fd)
	if sb.Len() != 0 {
		t.Errorf("expected empty output for unexported func, got %q", sb.String())
	}
}

// TestWriteSkeletonDecl_FormatError documents the intentional silent-skip
// behavior of writeGenDeclSkeleton and writeFuncDeclSkeleton when
// format.Node fails. This is the same silent-swallow pattern that was
// fixed in isDeclEqual (SILENT-1), but here the impact is lower: skeleton
// rendering is diagnostic, not critical to correctness.
//
// The "skip on format error" path is purely defensive. format.Node only
// returns errors for truly unsupported node types (e.g., nil), and these
// functions always construct valid AST nodes from parsed sources. Invalid
// constructions such as a nil TypeSpec.Type or nil FuncDecl.Type cause
// panics in the printer — not errors — and are not reachable from parsed
// Go source code.
//
// This test verifies the normal path (format succeeds for valid nodes)
// and independently tests the format.Node error contract with nil.
func TestWriteSkeletonDecl_FormatError(t *testing.T) {
	t.Parallel()
	cache := newASTCache(".")
	fset := token.NewFileSet()

	t.Run("writeGenDeclSkeleton produces output for valid type spec", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		gd := &ast.GenDecl{
			Tok: token.TYPE,
			Specs: []ast.Spec{
				&ast.TypeSpec{Name: &ast.Ident{Name: "Exported"}, Type: &ast.Ident{Name: "int"}},
			},
		}
		// Must not panic
		cache.writeGenDeclSkeleton(&sb, fset, gd)
		// Valid GenDecl produces skeleton output
		if sb.Len() == 0 {
			t.Error("expected non-empty skeleton output for valid GenDecl")
		}
	})

	t.Run("writeFuncDeclSkeleton produces output for valid func decl", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		fd := &ast.FuncDecl{
			Name: &ast.Ident{Name: "ExportedFunc"},
			Type: &ast.FuncType{Params: &ast.FieldList{}},
		}
		// Must not panic
		cache.writeFuncDeclSkeleton(&sb, fset, fd)
		// Valid FuncDecl produces skeleton output
		if sb.Len() == 0 {
			t.Error("expected non-empty skeleton output for valid FuncDecl")
		}
	})

	t.Run("format.Node returns error for nil node, no panic", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		// format.Node(nil node) returns an error, does not panic.
		// This is the error path the skeleton functions' silent-skip
		// pattern is designed for.
		err := format.Node(&sb, token.NewFileSet(), nil)
		if err == nil {
			t.Fatal("expected error from format.Node with nil node")
		}
		if sb.Len() != 0 {
			t.Errorf("expected empty output on format error, got %q", sb.String())
		}
	})
}

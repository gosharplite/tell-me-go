// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"go/ast"
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
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.go")
	content := "package main\nfunc main() {}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("Get", func(t *testing.T) { t.Parallel(); testASTCacheGet(t, path) })
	t.Run("Hit", func(t *testing.T) { t.Parallel(); testASTCacheHit(t, path) })
	t.Run("Invalidation", func(t *testing.T) { t.Parallel(); testASTCacheInvalidation(t, path) })
	t.Run("NonExistent", func(t *testing.T) { t.Parallel(); testASTCacheNonExistent(t) })
	t.Run("SyntaxError", func(t *testing.T) { t.Parallel(); testASTCacheSyntaxError(t, tmpDir) })
	t.Run("Eviction", func(t *testing.T) { t.Parallel(); testASTCacheEviction(t, tmpDir) })
}

func testASTCacheGet(t *testing.T, path string) {
	cache := newASTCache()
	f, _, err := cache.Get(path)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if f.Name.Name != "main" {
		t.Errorf("expected package main, got %s", f.Name.Name)
	}
}

func testASTCacheHit(t *testing.T, path string) {
	cache := newASTCache()
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
	cache := newASTCache()
	f1, _, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond) // Ensure modTime changes
	if err := os.WriteFile(path, []byte("package main\nfunc main() { _ = 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	f2, _, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	if f1 == f2 {
		t.Error("expected cache invalidation after file update")
	}
}

func testASTCacheNonExistent(t *testing.T) {
	cache := newASTCache()
	_, _, err := cache.Get("non_existent.go")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func testASTCacheSyntaxError(t *testing.T, tmpDir string) {
	cache := newASTCache()
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
	cache := newASTCache()
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

	cache := newASTCache()
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

	cache := newASTCache()
	info, _ := os.Stat(path)

	// 1. Initially not in cache
	count, ok := cache.GetCachedLineCount(path, info)
	if ok {
		t.Errorf("expected not in cache, but got count %d", count)
	}

	// 2. Parse and cache
	_, _, err := cache.Get(path)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Now it should be in cache
	count, ok = cache.GetCachedLineCount(path, info)
	if !ok {
		t.Error("expected to be in cache")
	}
	if count != 5 {
		t.Errorf("expected 5 lines, got %d", count)
	}

	// 4. Invalidation check
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
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
		cache := newASTCache()
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

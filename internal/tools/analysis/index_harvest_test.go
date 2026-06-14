// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/tools/go/packages"
)

func TestSendResult_ContextAlreadyCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := make(chan pkgResult)
	hb := make(chan struct{}, 1)

	err := sendResult(ctx, results, pkgResult{
		symbols: make(map[string][]symbolLocation),
		usages:  make(map[string][]location),
	}, hb)
	assert.ErrorIs(t, err, context.Canceled)

	select {
	case <-results:
		t.Error("unexpected result on results channel when context cancelled")
	default:
	}
	select {
	case <-hb:
		t.Error("unexpected heartbeat on hb channel when context cancelled")
	default:
	}
}

func TestSendResult_SuccessfulSendWithNilHb(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	results := make(chan pkgResult, 1)
	emptyResult := pkgResult{
		symbols: make(map[string][]symbolLocation),
		usages:  make(map[string][]location),
	}

	err := sendResult(ctx, results, emptyResult, nil)
	assert.NoError(t, err)

	select {
	case got := <-results:
		assert.Equal(t, emptyResult, got)
	default:
		t.Error("expected result on results channel")
	}
}

func TestSendResult_SuccessfulSendWithNonNilHb(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	results := make(chan pkgResult, 1)
	hb := make(chan struct{}, 1)
	emptyResult := pkgResult{
		symbols: make(map[string][]symbolLocation),
		usages:  make(map[string][]location),
	}

	err := sendResult(ctx, results, emptyResult, hb)
	assert.NoError(t, err)

	select {
	case got := <-results:
		assert.Equal(t, emptyResult, got)
	default:
		t.Error("expected result on results channel")
	}
	select {
	case <-hb:
	default:
		t.Error("expected heartbeat on hb channel")
	}
}

func TestSendResult_HbChannelFull(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	results := make(chan pkgResult, 1)
	hb := make(chan struct{})
	emptyResult := pkgResult{
		symbols: make(map[string][]symbolLocation),
		usages:  make(map[string][]location),
	}

	err := sendResult(ctx, results, emptyResult, hb)
	assert.NoError(t, err)

	select {
	case got := <-results:
		assert.Equal(t, emptyResult, got)
	default:
		t.Error("expected result on results channel")
	}
	select {
	case <-hb:
		t.Error("unexpected heartbeat on unbuffered hb (should have been dropped)")
	default:
	}
}

func TestSendResult_ResultsChannelFull_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan pkgResult)
	emptyResult := pkgResult{
		symbols: make(map[string][]symbolLocation),
		usages:  make(map[string][]location),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- sendResult(ctx, results, emptyResult, nil) }()

	cancel() // Cancel immediately
	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled)

	select {
	case <-results:
		t.Error("unexpected result on unbuffered results channel after cancellation")
	default:
	}
}

// mockFileInfo implements os.FileInfo for shouldIndexFile tests.
type mockFileInfo struct {
	isDir bool
}

func (m mockFileInfo) Name() string       { return "" }
func (m mockFileInfo) Size() int64        { return 0 }
func (m mockFileInfo) Mode() os.FileMode  { return 0 }
func (m mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() interface{}   { return nil }

func TestShouldIndexFile(t *testing.T) {
	t.Parallel()

	idx := &indexer{}

	tests := []struct {
		name     string
		path     string
		isDir    bool
		expected bool
	}{
		{
			name:     "regular Go file",
			path:     "/some/path/file.go",
			isDir:    false,
			expected: true,
		},
		{
			name:     "directory",
			path:     "/some/path",
			isDir:    true,
			expected: false,
		},
		{
			name:     "non-Go file (.txt)",
			path:     "/some/path/file.txt",
			isDir:    false,
			expected: false,
		},
		{
			name:     "no extension",
			path:     "/some/path/file",
			isDir:    false,
			expected: false,
		},
		{
			name:     "hidden Go file",
			path:     "/some/path/.hidden.go",
			isDir:    false,
			expected: true,
		},
		{
			name:     "uppercase extension",
			path:     "/some/path/file.GO",
			isDir:    false,
			expected: false,
		},
		{
			name:     "file named just .go",
			path:     "/some/path/.go",
			isDir:    false,
			expected: true,
		},
		{
			name:     "empty path",
			path:     "",
			isDir:    false,
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := mockFileInfo{isDir: tt.isDir}
			got := idx.shouldIndexFile(tt.path, info)
			if got != tt.expected {
				t.Errorf("shouldIndexFile(%q, isDir=%v) = %v; want %v",
					tt.path, tt.isDir, got, tt.expected)
			}
		})
	}
}

// TestProcessFile_ValidGoFile verifies that processFile correctly indexes
// a valid Go source file: the harvester's currentPath is set to the
// absolute path of the file, and symbols are collected.
func TestProcessFile_ValidGoFile(t *testing.T) {
	t.Parallel()
	idx := &indexer{resolvePath: filepath.Abs}

	tmpDir := t.TempDir()
	goFile := filepath.Join(tmpDir, "test.go")
	content := "package p\n\nvar X = 1\n"
	if err := os.WriteFile(goFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, goFile, content, 0)
	if err != nil {
		t.Fatal(err)
	}

	h := newHarvester(fset)
	err = idx.processFile(fset, file, h)
	if err != nil {
		t.Fatalf("processFile: %v", err)
	}

	absPath, _ := filepath.Abs(goFile)
	if h.currentPath != absPath {
		t.Errorf("currentPath = %q; want %q", h.currentPath, absPath)
	}
	if len(h.symbolsByPath) == 0 {
		t.Error("expected symbols to be collected")
	}
}

// TestProcessFile_OsStatFails verifies that processFile silently skips
// a file when os.Stat fails (e.g. file deleted between Load and Refresh).
// No error is returned, currentPath stays empty, and no symbols are recorded.
func TestProcessFile_OsStatFails(t *testing.T) {
	t.Parallel()
	idx := &indexer{resolvePath: filepath.Abs}

	fset := token.NewFileSet()
	path := "/nonexistent/deleted_file.go"
	file, err := parser.ParseFile(fset, path, "package p", 0)
	if err != nil {
		t.Fatal(err)
	}

	h := newHarvester(fset)
	err = idx.processFile(fset, file, h)
	if err != nil {
		t.Errorf("expected nil error for deleted file, got %v", err)
	}
	if h.currentPath != "" {
		t.Errorf("currentPath = %q; want empty", h.currentPath)
	}
	if len(h.symbolsByPath) != 0 {
		t.Error("expected no symbols for deleted file")
	}
}

// TestProcessFile_ShouldIndexFileReturnsFalse verifies that processFile
// returns early without error when shouldIndexFile returns false (e.g.
// for a non-.go file). currentPath stays empty and no symbols are recorded.
func TestProcessFile_ShouldIndexFileReturnsFalse(t *testing.T) {
	t.Parallel()
	idx := &indexer{resolvePath: filepath.Abs}

	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(txtFile, []byte("not a go file"), 0644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, txtFile, "package p", 0)
	if err != nil {
		t.Fatal(err)
	}

	h := newHarvester(fset)
	err = idx.processFile(fset, file, h)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if h.currentPath != "" {
		t.Errorf("currentPath = %q; want empty", h.currentPath)
	}
	if len(h.symbolsByPath) != 0 {
		t.Error("expected no symbols for non-.go file")
	}
}

// EC5: empty package, no Syntax
func TestProcessPackage_EmptyPackage(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	pkg := types.NewPackage("example.com/test", "test")
	manualPkg := &packages.Package{
		Types:     pkg,
		TypesInfo: &types.Info{},
		Syntax:    nil,
	}

	idx := &indexer{resolvePath: filepath.Abs}
	ctx := context.Background()
	result, err := idx.processPackage(ctx, fset, manualPkg)
	assert.NoError(t, err)
	assert.Empty(t, result.symbols)
	assert.Empty(t, result.usages)
}

// EC2: context cancelled before loop
func TestProcessPackage_ContextCancelled(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package test\nvar X = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedName,
		Dir:  tmpDir,
		Fset: fset,
	}, ".")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("expected at least one package")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	idx := &indexer{resolvePath: filepath.Abs}
	_, err = idx.processPackage(ctx, fset, pkgs[0])
	assert.ErrorIs(t, err, context.Canceled)
}

// EC3: processFile error propagation
func TestProcessPackage_ProcessFileError(t *testing.T) {
	t.Parallel()

	errInjected := errors.New("path resolution failed")
	idx := &indexer{
		resolvePath: func(string) (string, error) { return "", errInjected },
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "any.go", "package test\nvar X = 1\n", 0)
	if err != nil {
		t.Fatal(err)
	}

	h := newHarvester(fset)
	err = idx.processFile(fset, file, h)
	assert.ErrorIs(t, err, errInjected)
	assert.Empty(t, h.symbolsByPath)
	assert.Empty(t, h.currentPath)
}

// EC1: multiple files processed
func TestProcessPackage_MultipleFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package test\nvar A = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("package test\nvar B = 2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedName,
		Dir:  tmpDir,
		Fset: fset,
	}, ".")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("expected at least one package")
	}

	idx := &indexer{resolvePath: filepath.Abs}
	ctx := context.Background()
	result, err := idx.processPackage(ctx, fset, pkgs[0])
	assert.NoError(t, err)
	assert.Len(t, result.symbols, 2, "expected symbols from 2 files")
}

func TestHarvestPackages(t *testing.T) {
	t.Parallel()

	// -------------------------------------------------------------------
	// EC1: single package, success
	// -------------------------------------------------------------------
	t.Run("single package, success", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\ngo 1.26\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package test\nvar X = 1\n"), 0644); err != nil {
			t.Fatal(err)
		}

		fset := token.NewFileSet()
		pkgs, err := packages.Load(&packages.Config{
			Mode: packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedName,
			Dir:  tmpDir,
			Fset: fset,
		}, ".")
		if err != nil {
			t.Fatalf("packages.Load: %v", err)
		}
		if len(pkgs) == 0 {
			t.Fatal("expected at least one package")
		}

		idx := &indexer{resolvePath: filepath.Abs}
		ctx := context.Background()

		symbols, usages, err := idx.harvestPackages(ctx, fset, pkgs, nil)

		assert.NoError(t, err)
		assert.NotEmpty(t, symbols, "expected non-empty symbols map")
		// usages may be empty if no cross-references
		_ = usages
	})

	// -------------------------------------------------------------------
	// EC2: empty package list
	// -------------------------------------------------------------------
	t.Run("empty package list", func(t *testing.T) {
		t.Parallel()

		idx := &indexer{resolvePath: filepath.Abs}
		fset := token.NewFileSet()
		ctx := context.Background()

		symbols, usages, err := idx.harvestPackages(ctx, fset, nil, nil)

		assert.NoError(t, err)
		assert.Empty(t, symbols)
		assert.Empty(t, usages)
	})

	// -------------------------------------------------------------------
	// EC5: context cancelled — errgroup propagation
	//
	// A cancelled context causes sem.Acquire(gCtx, 1) to return
	// context.Canceled immediately.  The errgroup goroutine returns
	// that error, and g.Wait() propagates it to the caller.
	// -------------------------------------------------------------------
	t.Run("context cancelled, errgroup propagation", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\ngo 1.26\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package test\nvar X = 1\n"), 0644); err != nil {
			t.Fatal(err)
		}

		fset := token.NewFileSet()
		pkgs, err := packages.Load(&packages.Config{
			Mode: packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedName,
			Dir:  tmpDir,
			Fset: fset,
		}, ".")
		if err != nil {
			t.Fatalf("packages.Load: %v", err)
		}
		if len(pkgs) == 0 {
			t.Fatal("expected at least one package")
		}

		idx := &indexer{resolvePath: filepath.Abs}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		symbols, usages, err := idx.harvestPackages(ctx, fset, pkgs, nil)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, symbols)
		assert.Nil(t, usages)
	})
}

func TestIsExportTestFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func() (*token.FileSet, token.Pos)
		expected bool
	}{
		{
			name: "nil fset",
			setup: func() (*token.FileSet, token.Pos) {
				return nil, token.NoPos
			},
			expected: false,
		},
		{
			name: "invalid pos (NoPos)",
			setup: func() (*token.FileSet, token.Pos) {
				return token.NewFileSet(), token.NoPos
			},
			expected: false,
		},
		{
			// EC5: The pos belongs to a different fset → fset.File(pos)
			// returns nil because token.Pos values are scoped to their
			// originating *token.FileSet.
			name: "pos from different fset — fset.File returns nil",
			setup: func() (*token.FileSet, token.Pos) {
				targetFset := token.NewFileSet()
				otherFset := token.NewFileSet()
				otherF := otherFset.AddFile("/tmp/export_test.go", -1, 10)
				return targetFset, otherF.Pos(0)
			},
			expected: false,
		},
		{
			// EC6: Only the basename matters — a nested path whose
			// basename is "export_test.go" must return true.
			name: "nested path — basename is export_test.go",
			setup: func() (*token.FileSet, token.Pos) {
				fset := token.NewFileSet()
				f := fset.AddFile("/some/deeply/nested/path/export_test.go", -1, 100)
				return fset, f.Pos(10)
			},
			expected: true,
		},
		{
			// EC7: filepath.Base("") returns "." — not "export_test.go".
			name: "empty filename — base is dot",
			setup: func() (*token.FileSet, token.Pos) {
				fset := token.NewFileSet()
				f := fset.AddFile("", -1, 10)
				return fset, f.Pos(0)
			},
			expected: false,
		},
		{
			// Negative control: a file whose basename is NOT
			// "export_test.go" must return false.
			name: "regular _test.go file — helpers_test.go",
			setup: func() (*token.FileSet, token.Pos) {
				fset := token.NewFileSet()
				f := fset.AddFile("/pkg/helpers_test.go", -1, 50)
				return fset, f.Pos(0)
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fset, pos := tt.setup()
			got := isExportTestFile(fset, pos)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestHarvestNamedMethods_DuplicateRegistration(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}
	fset := token.NewFileSet()
	state := &scanState{
		declarations: make(map[string]*symMeta),
	}

	// Create a named struct type with one exported method
	pkg := types.NewPackage("example.com/pkg", "pkg")
	structType := types.NewStruct(nil, nil)
	named := types.NewNamed(
		types.NewTypeName(token.NoPos, pkg, "MyStruct", structType),
		structType,
		nil,
	)
	// Add one exported method: func (s MyStruct) ExportedMethod()
	recv := types.NewVar(token.NoPos, pkg, "s", named)
	sig := types.NewSignatureType(recv, nil, nil, nil, nil, false)
	method := types.NewFunc(token.NoPos, pkg, "ExportedMethod", sig)
	named.AddMethod(method)

	// First call: method should be registered
	analyzer.harvestNamedMethods(named, fset, state)
	assert.Len(t, state.declarations, 1)

	// Second call: method already registered, should not duplicate
	analyzer.harvestNamedMethods(named, fset, state)
	assert.Len(t, state.declarations, 1)
}

func TestHarvestInterfaceMethods_DuplicateRegistration(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}
	fset := token.NewFileSet()
	state := &scanState{
		declarations: make(map[string]*symMeta),
	}

	// Create an interface type with one exported method
	pkg := types.NewPackage("example.com/pkg", "pkg")
	exportedMethod := types.NewFunc(token.NoPos, pkg, "ExportedMethod",
		types.NewSignatureType(nil, nil, nil, nil, nil, false))
	itf := types.NewInterfaceType([]*types.Func{exportedMethod}, nil)

	// First call: method should be registered
	analyzer.harvestInterfaceMethods(itf, fset, state)
	assert.Len(t, state.declarations, 1)

	// Second call: method already registered, should not duplicate
	analyzer.harvestInterfaceMethods(itf, fset, state)
	assert.Len(t, state.declarations, 1)
}

// ---------------------------------------------------------------------------
// handleIdent edge‑case tests
// ---------------------------------------------------------------------------

func TestHandleIdent_NilInfo(t *testing.T) {
	t.Parallel()

	// EC1: h.info == nil → early return, no panic, no side-effects.
	h := newHarvester(token.NewFileSet()) // info is nil by default
	h.currentPath = "/tmp/test.go"
	ident := &ast.Ident{Name: "SomeFunc", NamePos: 1}

	h.handleIdent(ident)
	assert.Empty(t, h.usagesByName)
}

func TestHandleIdent_NotInUses(t *testing.T) {
	t.Parallel()

	// EC2: ident not in h.info.Uses map → early return (pure definition).
	fset := token.NewFileSet()
	h := newHarvester(fset)
	h.currentPath = "/tmp/test.go"
	h.info = &types.Info{
		Uses: make(map[*ast.Ident]types.Object),
	}
	ident := &ast.Ident{Name: "NotUsed", NamePos: 1}

	h.handleIdent(ident)
	assert.Empty(t, h.usagesByName)
}

func TestHandleIdent_UnexportedNonPackageLevel(t *testing.T) {
	t.Parallel()

	// EC3: ident IS in Uses but the object is unexported, non-method, and
	// non-package-level → early return.
	fset := token.NewFileSet()
	f := fset.AddFile("/tmp/test.go", -1, 100)
	pos := f.Pos(10)

	h := newHarvester(fset)
	h.currentPath = "/tmp/test.go"

	// localVar.Pkg() == nil → classifyObject returns all false.
	localVar := types.NewVar(pos, nil, "localVar", types.Typ[types.Int])

	ident := &ast.Ident{Name: "localVar", NamePos: pos}
	h.info = &types.Info{
		Uses: map[*ast.Ident]types.Object{ident: localVar},
	}

	h.handleIdent(ident)
	assert.Empty(t, h.usagesByName)
}

// TestHarvestPackageSymbols_AbsFails verifies that when filepath.Abs fails
// (e.g. due to a null byte in the path), harvestPackageSymbols falls back
// to the raw directory path instead of silently skipping the package.
func TestHarvestPackageSymbols_AbsFails(t *testing.T) {
	t.Parallel()

	analyzer := &defaultDeadCodeAnalyzer{}

	state := &scanState{
		targetModule: "example.com",
		targetPath:   "/some/valid/prefix",
		declarations: make(map[string]*symMeta),
	}

	// Create a package with a Go file path containing a null byte so that
	// filepath.Abs returns an error.  The raw dir path (from filepath.Dir)
	// will be "/some/valid/prefix" which passes the targetPath check.
	pkg := &packages.Package{
		PkgPath:   "example.com/test",
		GoFiles:   []string{"/some/valid/prefix/pkg\x00.go"},
		Types:     types.NewPackage("example.com/test", "test"),
		TypesInfo: &types.Info{},
		Module: &packages.Module{
			Path:    "example.com",
			Version: "v0.0.1",
		},
	}

	// Insert an exported symbol so that the scope iteration path is exercised.
	exportedVar := types.NewVar(token.NoPos, pkg.Types, "ExportedVar", types.Typ[types.Int])
	pkg.Types.Scope().Insert(exportedVar)

	analyzer.harvestPackageSymbols(pkg, state)

	// No panic and the exported symbol should be registered.
	assert.NotEmpty(t, state.declarations, "expected exported symbol to be harvested")
}

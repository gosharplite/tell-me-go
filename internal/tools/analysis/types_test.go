package analysis

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// newTypesWorkspaceIndexer creates a fresh, per-test workspace containing
// all test scenarios as sub-packages under a single Go module. Each call
// uses t.TempDir() so parallel tests never share the same directory tree.
func newTypesWorkspaceIndexer(t testing.TB) (string, *indexer) {
	t.Helper()
	const knownModulePath = "shared.types"

	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "types-shared")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	// Write a single top-level go.mod
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+knownModulePath+"\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Each entry maps a sub-directory name to its Go source content.
	workspaceFiles := map[string]string{
		"get_type_info_basic_struct/test.go": `package basicstruct
// MyStruct is a test struct
type MyStruct struct {
	ID int ` + "`json:\"id\"`" + `
}
func (s *MyStruct) Foo() {}
`,
		"get_type_info_interface/test.go": `package ifacetype
// Reader is an interface
type Reader interface {
	Read(p []byte) (n int, err error)
}`,
		"get_type_info_alias/test.go": `package aliastype
type MyInt int`,
		"get_type_info_embedding/test.go": `package embedtype
type I1 interface { M1() }
type I2 interface {
	I1
	M2()
}`,
		// empty file — used by "empty typename" subtest
		"get_type_info_empty/test.go": `package emptytype
`,
		// empty file — used by "missing type" subtest
		"get_type_info_missing/test.go": `package missingtype
`,
		"list_implementations/test.go": `package listimpl
type I interface { M() }
type S struct{}
func (s S) M() {}
`,
		"list_symbols/test.go": `package listsym
func F() {}
type T struct{}
var V int
const C = 1
`,
		"find_usages/test.go": `package finduse
func F() { F() }
`,
		"find_definitions/test.go": `package finddef
func MyFunc() {}
`,
		"list_symbols_exported/test.go": `package listsymexp
func Exported() {}
func unexported() {}
`,
	}

	for path, content := range workspaceFiles {
		fullPath := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	idx, err := newIndexer(root)
	if err != nil {
		t.Fatal(err)
	}
	idx.knownModulePath = knownModulePath
	if err := idx.Refresh(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return root, idx
}

func TestTypeManager_GetTypeInfo(t *testing.T) {
	t.Parallel()

	root, idx := newTypesWorkspaceIndexer(t)
	cache := newASTCache(".")
	sp := &mockSecurityProvider{}
	m := newTypeManager(idx, cache, sp, infra_persistence.NewOSFileSystem())

	tests := []struct {
		name     string
		dir      string
		typename string
		want     []string
		wantErr  bool
	}{
		{
			name:     "basic struct",
			dir:      "get_type_info_basic_struct",
			typename: "MyStruct",
			want:     []string{"Type: MyStruct", "ID int `json:\"id\"`", "func (s *MyStruct) Foo()"},
		},
		{
			name:     "interface type",
			dir:      "get_type_info_interface",
			typename: "Reader",
			want:     []string{"Type: Reader", "Methods:", "Read"},
		},
		{
			name:     "type alias",
			dir:      "get_type_info_alias",
			typename: "MyInt",
			want:     []string{"Type: MyInt", "Kind: alias"},
		},
		{
			name:     "interface type with embedding",
			dir:      "get_type_info_embedding",
			typename: "I2",
			want:     []string{"Type: I2", "M2"},
		},
		{
			name: "empty typename",
			dir:  "get_type_info_empty",
			want: []string{"Please provide a typename."},
		},
		{
			name:     "missing type",
			dir:      "get_type_info_missing",
			typename: "Missing",
			want:     []string{"Type not found."},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			caseDir := filepath.Join(root, tt.dir)
			args := map[string]interface{}{
				"typename": tt.typename,
				"path":     caseDir,
			}

			res, err := m.GetTypeInfo(context.Background(), args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTypeInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for _, w := range tt.want {
				if !strings.Contains(res.Text, w) {
					t.Errorf("Expected result to contain %q. Got:\n%s", w, res.Text)
				}
			}
		})
	}
}

func TestTypeManager_ListImplementations(t *testing.T) {
	t.Parallel()

	_, idx := newTypesWorkspaceIndexer(t)
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())

	res, err := m.ListImplementations(context.Background(), map[string]interface{}{"interface_name": "I"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "S") {
		t.Errorf("Expected result to contain S, got:\n%s", res.Text)
	}
}

func TestTypeManager_ListSymbols(t *testing.T) {
	t.Parallel()

	root, idx := newTypesWorkspaceIndexer(t)
	caseDir := filepath.Join(root, "list_symbols")
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())
	res, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": caseDir}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []string{"func F()", "type T", "var V", "C"} {
		if !strings.Contains(res.Text, s) {
			t.Errorf("Expected symbol %q in output, got:\n%s", s, res.Text)
		}
	}
}

func TestTypeManager_FindUsages(t *testing.T) {
	t.Parallel()

	root, idx := newTypesWorkspaceIndexer(t)
	caseDir := filepath.Join(root, "find_usages")
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())
	res, err := m.FindUsages(context.Background(), map[string]interface{}{"path": caseDir, "query": "F"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(res.Text, "test.go") < 1 {
		t.Errorf("Expected at least 1 usage of F, got:\n%s", res.Text)
	}
}

func TestTypeManager_FindDefinitions(t *testing.T) {
	t.Parallel()

	root, idx := newTypesWorkspaceIndexer(t)
	caseDir := filepath.Join(root, "find_definitions")
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())
	res, err := m.FindDefinitions(context.Background(), map[string]interface{}{"path": caseDir, "query": "MyFunc"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "func MyFunc()") {
		t.Errorf("Expected definition of MyFunc, got:\n%s", res.Text)
	}
}

func TestTypeManager_ListImplementations_Error(t *testing.T) {
	t.Parallel()
	idx, _ := newIndexer(".")
	m := newTypeManager(idx, nil, &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())
	res, err := m.ListImplementations(context.Background(), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Please provide an interface_name.") {
		t.Errorf("Expected error message for missing interface_name")
	}
}

func TestComplexityAnalyzer_Analyze_Empty(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	analyzer := newComplexityAnalyzer(newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())
	res, err := analyzer.Analyze(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "No Go functions found to analyze.") {
		t.Errorf("Expected empty result message, got: %s", res.Text)
	}
}

func TestTypeManager_ListSymbols_ExportedOnly(t *testing.T) {
	t.Parallel()

	root, idx := newTypesWorkspaceIndexer(t)
	caseDir := filepath.Join(root, "list_symbols_exported")
	m := newTypeManager(idx, newASTCache("."), &mockSecurityProvider{}, infra_persistence.NewOSFileSystem())
	res, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": caseDir, "exported_only": true}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "func Exported()") {
		t.Errorf("Expected Exported in output")
	}
	if strings.Contains(res.Text, "unexported") {
		t.Errorf("Did not expect unexported in output")
	}
}

func TestRenderTypeInfo_Fields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fields   []fieldInfo
		want     string   // exact expected output (for "output integrity" case)
		contains []string // substrings the output must contain
		absent   []string // substrings the output must NOT contain
	}{
		{
			name:   "empty fields",
			fields: nil,
			absent: []string{"Fields:"},
		},
		{
			name:     "single field no tag",
			fields:   []fieldInfo{{Names: "ID", Type: "int"}},
			contains: []string{"Fields:\n", "  - ID int\n"},
			absent:   []string{"`"},
		},
		{
			name:   "single field with tag",
			fields: []fieldInfo{{Names: "Name", Type: "string", Tag: "`json:\"name\"`"}},
			contains: []string{
				"Fields:\n",
				"  - Name string `json:\"name\"`\n",
			},
		},
		{
			name: "multiple fields mixed tags",
			fields: []fieldInfo{
				{Names: "A", Type: "int"},
				{Names: "B", Type: "string", Tag: "`xml:\"b\"`"},
			},
			contains: []string{
				"  - A int\n",
				"  - B string `xml:\"b\"`\n",
			},
		},
		{
			name:   "output integrity",
			fields: []fieldInfo{{Names: "ID", Type: "int", Tag: "`json:\"id\"`"}},
			want:   "Fields:\n  - ID int `json:\"id\"`\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTypeManager(nil, nil, nil, infra_persistence.NewOSFileSystem())
			def := typeDefinition{Fields: tt.fields}
			var sb strings.Builder
			m.renderTypeInfo_fields(def, &sb)
			got := sb.String()

			if tt.want != "" {
				if got != tt.want {
					t.Errorf("got %q; want %q", got, tt.want)
				}
				return
			}

			for _, w := range tt.contains {
				if !strings.Contains(got, w) {
					t.Errorf("expected output to contain %q. Got:\n%s", w, got)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("expected output to NOT contain %q. Got:\n%s", a, got)
				}
			}
		})
	}
}

func TestTypeManager_ErrorPaths(t *testing.T) {
	t.Parallel()

	// denyingSP rejects all paths
	denyingSP := &denyingSecurityProvider{}

	t.Run("FindUsages UnmarshalArgs error", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{SP: &mockSecurityProvider{}}
		_, err := m.FindUsages(context.Background(), map[string]interface{}{"query": make(chan int)}, nil)
		if err == nil {
			t.Error("expected error for unserializable args")
		}
	})

	t.Run("FindUsages IsPathSafe error", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{SP: denyingSP}
		_, err := m.FindUsages(context.Background(), map[string]interface{}{"query": "Foo"}, nil)
		if err == nil {
			t.Error("expected error from IsPathSafe rejection")
		}
	})

	t.Run("FindUsages GetUsages error", func(t *testing.T) {
		t.Parallel()
		mockIdx := &mockSymbolIndex{
			GetUsagesFunc: func(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]location, error) {
				return nil, fmt.Errorf("index lookup failed")
			},
		}
		m := &defaultTypeManager{SP: &mockSecurityProvider{}, Indexer: mockIdx}
		_, err := m.FindUsages(context.Background(), map[string]interface{}{"query": "Foo", "path": "."}, nil)
		if err == nil {
			t.Error("expected error from GetUsages")
		}
	})

	t.Run("FindUsages no results", func(t *testing.T) {
		t.Parallel()
		mockIdx := &mockSymbolIndex{
			GetUsagesFunc: func(ctx context.Context, symbol string, path string, hb chan<- struct{}) ([]location, error) {
				return nil, nil
			},
		}
		m := &defaultTypeManager{SP: &mockSecurityProvider{}, Indexer: mockIdx}
		result, err := m.FindUsages(context.Background(), map[string]interface{}{"query": "Foo", "path": "."}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result.Text, "No usages found") {
			t.Errorf("expected 'No usages found', got %q", result.Text)
		}
	})

	t.Run("FindDefinitions UnmarshalArgs error", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{SP: &mockSecurityProvider{}}
		_, err := m.FindDefinitions(context.Background(), map[string]interface{}{"query": make(chan int)}, nil)
		if err == nil {
			t.Error("expected error for unserializable args")
		}
	})

	t.Run("FindDefinitions resolvePath error", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{SP: denyingSP}
		_, err := m.FindDefinitions(context.Background(), map[string]interface{}{"query": "Foo"}, nil)
		if err == nil {
			t.Error("expected error from resolvePath rejection")
		}
	})

	t.Run("ListSymbols UnmarshalArgs error", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{SP: &mockSecurityProvider{}}
		_, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": make(chan int)}, nil)
		if err == nil {
			t.Error("expected error for unserializable args")
		}
	})

	t.Run("ListSymbols resolvePath error", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{SP: denyingSP}
		_, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": "/some/path"}, nil)
		if err == nil {
			t.Error("expected error from resolvePath rejection")
		}
	})

	t.Run("handleHeartbeat nil count does not panic", func(t *testing.T) {
		t.Parallel()
		m := &defaultTypeManager{}
		// Must not panic when count is nil.
		m.handleHeartbeat(nil, nil)
	})
}

// callbackWalkErrorFS calls the walk callback exactly once with a
// non-nil walkErr, exercising the walkErr != nil propagation path
// in makeMethodWalkFunc and collectSymbols.
type callbackWalkErrorFS struct {
	persistence.FileSystem
}

func (c *callbackWalkErrorFS) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return fn("/some/path", nil, fs.ErrPermission)
}

// TestMakeMethodWalkFunc_WalkError verifies that a walk callback receiving
// a non-nil walkErr (e.g., permission denied from filepath.Walk) propagates
// the error directly without further processing.
func TestMakeMethodWalkFunc_WalkError(t *testing.T) {
	t.Parallel()
	mfs := &callbackWalkErrorFS{FileSystem: persistence.NewMockFileSystem()}
	tm := newTypeManager(&mockSymbolIndex{}, newASTCache("."), &mockSecurityProvider{}, mfs)
	_, err := tm.findMethodsInPackage(context.Background(), "/some/path", "SomeType", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrPermission))
}

// TestMakeMethodWalkFunc_ContextCancelled verifies that when the context
// is cancelled during a method walk, checkCancellation returns the context
// error and the walk aborts cleanly.
func TestMakeMethodWalkFunc_ContextCancelled(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "f.go"), []byte("package pkg"), 0644))

	// contextFreeWalkFS (defined in dependency_test.go) bypasses
	// domainFS.Walk's ctx.Err() pre-check so the callback actually fires.
	osFS := infra_persistence.NewOSFileSystem()
	cfs := &contextFreeWalkFS{FileSystem: osFS}

	tm := newTypeManager(&mockSymbolIndex{}, newASTCache("."), &mockSecurityProvider{}, cfs)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := tm.findMethodsInPackage(ctx, tmpDir, "SomeType", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

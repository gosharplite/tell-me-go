package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexer_SymbolClassification(t *testing.T) {
	t.Parallel()
	code := `package test
type MyStruct struct{}
func (s *MyStruct) MyPointerMethod() {}
func (s MyStruct) MyValueMethod() {}
type MyInterface interface { M() }
func MyFunc() {}
var MyVar = 1
const MyConst = 2
type MyAlias = int
func unexported() {}
`
	tmpDir, idx := setupIndexerWorkspace(t, code)
	ctx := context.Background()

	tests := []struct {
		name     string
		kind     string
		receiver string
	}{
		{"MyPointerMethod", "func", "*MyStruct"},
		{"MyValueMethod", "func", "MyStruct"},
		{"MyFunc", "func", ""},
		{"MyVar", "var", ""},
		{"MyConst", "const", ""},
		{"MyStruct", "type", ""},
		{"MyAlias", "type", ""},
		{"MyInterface", "type", ""},
		{"unexported", "func", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			syms, err := idx.SearchSymbols(ctx, tmpDir, tt.name, false, nil)
			require.NoError(t, err)

			var found *symbolLocation
			for i := range syms {
				if syms[i].Name == tt.name {
					found = &syms[i]
					break
				}
			}

			require.NotNil(t, found, "symbol %s not found", tt.name)
			assert.Equal(t, tt.kind, found.Kind)
			assert.Equal(t, tt.receiver, found.Receiver)
		})
	}
}

func setupIndexerWorkspace(t *testing.T, code string) (string, *indexer) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644))

	idx, err := newIndexer(tmpDir)
	require.NoError(t, err)

	require.NoError(t, idx.Refresh(context.Background(), nil))

	return tmpDir, idx
}

func TestIndexer_FindImplementors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)
	code := `package test
type I interface { M() }
type S struct{}
func (s S) M() {}
`
	_ = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)

	idx, _ := newIndexer(tmpDir)
	ctx := context.Background()
	_ = idx.Refresh(ctx, nil)

	implementors, err := idx.FindImplementors(ctx, "I", nil)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, imp := range implementors {
		if imp.Name == "S" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected S to implement I")
	}
}

func TestIndexer_Lookup(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)
	code := `package test
type T struct{}
`
	_ = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)

	idx, _ := newIndexer(tmpDir)
	ctx := context.Background()
	_ = idx.Refresh(ctx, nil)

	locs, err := idx.Lookup(ctx, "T", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(locs) == 0 {
		t.Fatal("Expected to find location of T")
	}
	if !strings.HasSuffix(locs[0].Path, "test.go") {
		t.Errorf("Expected path to end with test.go, got %s", locs[0].Path)
	}
}

func TestIndexer_ErrorPersistence(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644)

	// Valid file
	_ = os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte("package test\nfunc F() {}"), 0644)

	idx, _ := newIndexer(tmpDir)
	ctx := context.Background()
	_ = idx.Refresh(ctx, nil)

	syms, _ := idx.SearchSymbols(ctx, tmpDir, "F", false, nil)
	if len(syms) != 1 {
		t.Fatal("Expected to find F")
	}

	// Create a file with syntax error
	_ = os.WriteFile(filepath.Join(tmpDir, "invalid.go"), []byte("package test\nfunc G() {"), 0644)

	// Force refresh by resetting lastRefresh
	idx.mu.Lock()
	idx.lastRefresh = time.Time{}
	idx.mu.Unlock()

	_ = idx.Refresh(ctx, nil)

	syms, _ = idx.SearchSymbols(ctx, tmpDir, "F", false, nil)
	if len(syms) == 0 {
		t.Error("Expected to still have F in despite other file errors")
	}
}

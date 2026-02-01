package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexer_SymbolClassification(t *testing.T) {
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	code := `package test

type MyStruct struct{}

// Method
func (s *MyStruct) MyMethod(a int) string { return "" }

// Function
func MyFunc() {}

// Variables and Constants
var MyVar = 1
const MyConst = 2

type MyAlias = int
`
	err = os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)
	if err != nil {
		t.Fatal(err)
	}

	idx, err := NewIndexer(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := idx.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		kind     string
		receiver string
	}{
		{"MyStruct", "type", ""},
		{"MyMethod", "func", "*MyStruct"},
		{"MyFunc", "func", ""},
		{"MyVar", "var", ""},
		{"MyConst", "const", ""},
		{"MyAlias", "type", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syms, err := idx.SearchSymbols(ctx, tmpDir, tt.name, false)
			if err != nil {
				t.Fatalf("SearchSymbols failed: %v", err)
			}

			found := false
			for _, s := range syms {
				if s.Name == tt.name {
					found = true
					if s.Kind != tt.kind {
						t.Errorf("expected kind %s, got %s", tt.kind, s.Kind)
					}
					if s.Receiver != tt.receiver {
						t.Errorf("expected receiver %q, got %q", tt.receiver, s.Receiver)
					}
				}
			}

			if !found {
				t.Errorf("symbol %s not found", tt.name)
			}
		})
	}
}

func TestIndexer_FindImplementors(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	code := `package test
type I interface { M() }
type S struct{}
func (s S) M() {}
`
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)

	idx, _ := NewIndexer(tmpDir)
	ctx := context.Background()
	idx.Refresh(ctx)

	implementors, err := idx.FindImplementors(ctx, "I")
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
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	code := `package test
type T struct{}
`
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(code), 0644)

	idx, _ := NewIndexer(tmpDir)
	ctx := context.Background()
	idx.Refresh(ctx)

	locs, err := idx.Lookup(ctx, "T")
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
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	
	// Valid file
	os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte("package test\nfunc F() {}"), 0644)
	
	idx, _ := NewIndexer(tmpDir)
	ctx := context.Background()
	idx.Refresh(ctx)
	
	syms, _ := idx.SearchSymbols(ctx, tmpDir, "F", false)
	if len(syms) != 1 {
		t.Fatal("Expected to find F")
	}

	// Create a file with syntax error
	os.WriteFile(filepath.Join(tmpDir, "invalid.go"), []byte("package test\nfunc G() {"), 0644)
	
	// Force refresh by resetting lastRefresh
	idx.mu.Lock()
	idx.lastRefresh = time.Time{}
	idx.mu.Unlock()
	
	_ = idx.Refresh(ctx)
	
	syms, _ = idx.SearchSymbols(ctx, tmpDir, "F", false)
	if len(syms) == 0 {
		t.Error("Expected to still have F in index despite other file errors")
	}
}

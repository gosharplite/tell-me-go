package analysis_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis/analysistest"
)

// mockTypeIndex embeds analysistest.MockSymbolIndex to inherit all
// symbolIndex method implementations, overriding only Lookup for
// test-specific control.
type mockTypeIndex struct {
	analysistest.MockSymbolIndex
	LookupFunc func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error)
}

func (m *mockTypeIndex) Lookup(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
	if m.LookupFunc != nil {
		return m.LookupFunc(ctx, symbol, hb)
	}
	return nil, nil
}

// errSentinel is a sentinel error used to verify Lookup error propagation.
var errSentinel = errors.New("index lookup failure")

// setupErrorPathTmpDir creates two Go source files in a temp directory:
//   - mismatch.go: contains OtherType (not MyStruct)
//   - hastype.go: contains MyStruct with method Foo
//
// Returns the temp directory path.
func setupErrorPathTmpDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	mismatchFile := filepath.Join(tmpDir, "mismatch.go")
	if err := os.WriteFile(mismatchFile, []byte(`package test
type OtherType struct {
	Name string
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	hasTypeFile := filepath.Join(tmpDir, "hastype.go")
	if err := os.WriteFile(hasTypeFile, []byte(`package test
type MyStruct struct {
	ID int
}
func (s *MyStruct) Foo() string { return "" }
`), 0644); err != nil {
		t.Fatal(err)
	}

	return tmpDir
}

// TestGetTypeInfo_UnmarshalArgsError verifies that an unmarshalable argument
// (a channel) produces an error and a zero-valued ToolResult.
func TestGetTypeInfo_UnmarshalArgsError(t *testing.T) {
	t.Parallel()
	m := analysis.NewTypeManager(&mockTypeIndex{}, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	ch := make(chan struct{})
	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": ch}, nil)
	if err == nil {
		t.Fatal("expected error for channel value, got nil")
	}
	if res.Text != "" {
		t.Errorf("expected empty Text on UnmarshalArgs error, got %q", res.Text)
	}
	if res.Metadata != nil || res.BinaryData != nil || res.Error != nil {
		t.Errorf("expected zero-valued ToolResult fields, got Metadata=%v BinaryData=%v Error=%v",
			res.Metadata, res.BinaryData, res.Error)
	}
}

// TestGetTypeInfo_EmptyTypename verifies that an empty typename returns
// a guidance message without error.
func TestGetTypeInfo_EmptyTypename(t *testing.T) {
	t.Parallel()
	m := analysis.NewTypeManager(&mockTypeIndex{}, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": ""}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Please provide a typename.") {
		t.Errorf("expected guidance message, got: %s", res.Text)
	}
}

// TestGetTypeInfo_LookupError verifies that when Lookup returns an error,
// the method returns a "Type not found." message without surfacing the error.
func TestGetTypeInfo_LookupError(t *testing.T) {
	t.Parallel()
	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return nil, errSentinel
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "Foo"}, nil)
	if err == nil {
		t.Fatal("expected error from Lookup failure, got nil")
	}
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected error to wrap errSentinel, got: %v", err)
	}
	if !strings.Contains(err.Error(), "lookup type Foo") {
		t.Errorf("expected error message to contain 'lookup type Foo', got: %v", err)
	}
	_ = res
}

// TestGetTypeInfo_LookupEmpty verifies that when Lookup returns zero locations
// without an error, the method returns a "Type not found." message.
func TestGetTypeInfo_LookupEmpty(t *testing.T) {
	t.Parallel()
	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return nil, nil
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "Foo"}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Type not found.") {
		t.Errorf("expected 'Type not found.', got: %s", res.Text)
	}
}

// TestGetTypeInfo_CacheGetError verifies that when the AST cache cannot
// retrieve a file (missing on disk), an error is propagated.
func TestGetTypeInfo_CacheGetError(t *testing.T) {
	t.Parallel()
	tmpDir := setupErrorPathTmpDir(t)

	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return []analysis.Location{{Path: filepath.Join(tmpDir, "does_not_exist.go"), Line: 1, Column: 1}}, nil
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	_, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "Foo"}, nil)
	if err == nil {
		t.Error("expected error for cache get failure, got nil")
	}
}

// TestGetTypeInfo_FindTypeSpecNil verifies that when findTypeSpec returns nil
// (the file parses but lacks the requested type), the method returns a
// "Type not found." message.
func TestGetTypeInfo_FindTypeSpecNil(t *testing.T) {
	t.Parallel()
	tmpDir := setupErrorPathTmpDir(t)

	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return []analysis.Location{{Path: filepath.Join(tmpDir, "mismatch.go"), Line: 2, Column: 6}}, nil
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "MissingType"}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Text, "Type not found.") {
		t.Errorf("expected 'Type not found.', got: %s", res.Text)
	}
}

// TestGetTypeInfo_FindMethodsError verifies that a cancelled context causes
// findMethodsInPackage to fail, propagating an error.
func TestGetTypeInfo_FindMethodsError(t *testing.T) {
	t.Parallel()
	tmpDir := setupErrorPathTmpDir(t)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return []analysis.Location{{Path: filepath.Join(tmpDir, "hastype.go"), Line: 2, Column: 6}}, nil
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	_, err := m.GetTypeInfo(cancelCtx, map[string]interface{}{"typename": "MyStruct"}, nil)
	if err == nil {
		t.Error("expected error for cancelled context during findMethods, got nil")
	}
}

// TestGetTypeInfo_ShortCircuit verifies that early-return conditions prevent
// downstream code from executing.
func TestGetTypeInfo_ShortCircuit(t *testing.T) {
	t.Parallel()

	t.Run("empty typename skips Lookup", func(t *testing.T) {
		t.Parallel()
		var lookupCalled bool
		idx := &mockTypeIndex{
			LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
				lookupCalled = true
				return nil, nil
			},
		}
		m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

		res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": ""}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lookupCalled {
			t.Error("Lookup was called but should have been short-circuited by empty typename check")
		}
		if !strings.Contains(res.Text, "Please provide a typename.") {
			t.Errorf("expected guidance message, got: %s", res.Text)
		}
	})

	t.Run("missing typename key defaults to empty string", func(t *testing.T) {
		t.Parallel()
		idx := &mockTypeIndex{}
		m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

		res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.Text, "Please provide a typename.") {
			t.Errorf("expected guidance message for missing typename key, got: %s", res.Text)
		}
	})

	t.Run("UnmarshalArgs error returns zero ToolResult", func(t *testing.T) {
		t.Parallel()
		m := analysis.NewTypeManager(&mockTypeIndex{}, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

		ch := make(chan struct{})
		res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": ch}, nil)
		if err == nil {
			t.Fatal("expected error for channel value, got nil")
		}
		if res.Text != "" {
			t.Errorf("expected empty Text on UnmarshalArgs error, got %q", res.Text)
		}
	})
}

// TestCollectSymbols_SkipsUnparseableFile verifies that collectSymbols
// gracefully tolerates a .go file with a syntax error and continues
// collecting symbols from valid files in the same tree.
func TestCollectSymbols_SkipsUnparseableFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Write a valid go.mod so the indexer can operate.
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}

	// valid.go: a well-formed Go file with one exported function.
	validSrc := filepath.Join(tmpDir, "valid.go")
	if err := os.WriteFile(validSrc, []byte(`package test

func ValidFunc() string { return "ok" }
`), 0644); err != nil {
		t.Fatal(err)
	}

	// broken.go: syntactically invalid Go.
	brokenSrc := filepath.Join(tmpDir, "broken.go")
	if err := os.WriteFile(brokenSrc, []byte(`package test

func broken() { // missing closing brace
`), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &analysistest.MockSymbolIndex{}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	res, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err != nil {
		t.Fatalf("ListSymbols should not fail when one file is unparseable: %v", err)
	}

	// ValidFunc from valid.go must appear.
	if !strings.Contains(res.Text, "ValidFunc") {
		t.Errorf("expected ValidFunc in output, got:\n%s", res.Text)
	}
	// broken.go symbols must not appear.
	if strings.Contains(res.Text, "broken") {
		t.Errorf("expected no symbols from broken.go, got:\n%s", res.Text)
	}
}

// TestFindMethodsInPackage_SkipsUnparseableFile verifies that
// findMethodsInPackage gracefully tolerates a .go file with a syntax
// error and still discovers methods from valid files in the same tree.
func TestFindMethodsInPackage_SkipsUnparseableFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}

	// valid.go: a struct with a method.
	if err := os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte(`package test

type MyStruct struct{}

func (s *MyStruct) Greet() string { return "hi" }
`), 0644); err != nil {
		t.Fatal(err)
	}

	// broken.go: syntactically invalid Go.
	if err := os.WriteFile(filepath.Join(tmpDir, "broken.go"), []byte(`package test

func broken() { // missing closing brace
`), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return []analysis.Location{{Path: filepath.Join(tmpDir, "valid.go"), Line: 2, Column: 6}}, nil
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	res, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "MyStruct"}, nil)
	if err != nil {
		t.Fatalf("GetTypeInfo should not fail when one file is unparseable: %v", err)
	}
	if !strings.Contains(res.Text, "Greet") {
		t.Errorf("expected method Greet in output, got:\n%s", res.Text)
	}
}

// TestCollectSymbols_PropagatesWalkError verifies that filesystem-level
// walk errors (e.g., permission denied) are propagated rather than
// silently skipped.
func TestCollectSymbols_PropagatesWalkError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	validSrc := filepath.Join(tmpDir, "valid.go")
	if err := os.WriteFile(validSrc, []byte(`package test

func ValidFunc() string { return "ok" }
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory and make it unreadable.
	lockedDir := filepath.Join(tmpDir, "locked")
	if err := os.Mkdir(lockedDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0700) })

	m := analysis.NewTypeManager(
		&analysistest.MockSymbolIndex{},
		analysis.NewASTCache("."),
		&analysistest.MockSecurityProvider{},
	)

	_, err := m.ListSymbols(context.Background(), map[string]interface{}{"path": tmpDir}, nil)
	if err == nil {
		t.Error("expected walk error from unreadable directory, got nil")
	}
}

// TestGetTypeInfo_PropagatesWalkError verifies that filesystem-level
// walk errors during method discovery propagate through GetTypeInfo.
func TestGetTypeInfo_PropagatesWalkError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "valid.go"), []byte(`package test

type MyStruct struct{}
`), 0644); err != nil {
		t.Fatal(err)
	}

	lockedDir := filepath.Join(tmpDir, "locked")
	if err := os.Mkdir(lockedDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0700) })

	idx := &mockTypeIndex{
		LookupFunc: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
			return []analysis.Location{{Path: filepath.Join(tmpDir, "valid.go"), Line: 2, Column: 6}}, nil
		},
	}
	m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})

	_, err := m.GetTypeInfo(context.Background(), map[string]interface{}{"typename": "MyStruct"}, nil)
	if err == nil {
		t.Error("expected walk error from unreadable directory during findMethodsInPackage, got nil")
	}
}

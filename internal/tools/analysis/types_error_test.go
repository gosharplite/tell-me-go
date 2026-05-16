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

// TestGetTypeInfo_ErrorPaths exercises all seven error/early-return paths in
// defaultTypeManager.GetTypeInfo. It uses a mock symbolIndex to control each
// decision point in isolation.
func TestGetTypeInfo_ErrorPaths(t *testing.T) {
	t.Parallel()

	// Pre-create shared temp files for findTypeSpec-nil and findMethodsInPackage-error cases.
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

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // immediately canceled

	type testCase struct {
		name     string
		args     map[string]interface{}
		lookupFn func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error)
		ctx      context.Context
		wantErr  bool
		wantText string // if non-empty, must appear in res.Text
	}

	cases := []testCase{
		// ── Path 1: UnmarshalArgs fails ──────────────────────────────────────
		// Channel values cannot be JSON-marshaled, so UnmarshalArgs returns an error.
		{
			name:    "path1_UnmarshalArgs_channel",
			args:    map[string]interface{}{"typename": make(chan int)},
			wantErr: true,
		},
		// ── Path 2: Empty typename ───────────────────────────────────────────
		{
			name:     "path2_empty_typename",
			args:     map[string]interface{}{"typename": ""},
			wantErr:  false,
			wantText: "Please provide a typename.",
		},
		// ── Path 3: Lookup returns error ─────────────────────────────────────
		// Code does: if err != nil || len(locs) == 0 → "Type not found."
		// The error is swallowed; the caller sees nil error.
		{
			name: "path3_Lookup_error",
			args: map[string]interface{}{"typename": "Foo"},
			lookupFn: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
				return nil, errSentinel
			},
			wantErr:  false,
			wantText: "Type not found.",
		},
		// ── Path 4: Lookup returns empty locations ───────────────────────────
		{
			name: "path4_Lookup_empty",
			args: map[string]interface{}{"typename": "Foo"},
			lookupFn: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
				return nil, nil
			},
			wantErr:  false,
			wantText: "Type not found.",
		},
		// ── Path 5: Cache.Get fails ──────────────────────────────────────────
		// Lookup returns a valid location but the file doesn't exist on disk.
		{
			name: "path5_CacheGet_error",
			args: map[string]interface{}{"typename": "Foo"},
			lookupFn: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
				return []analysis.Location{{Path: filepath.Join(tmpDir, "does_not_exist.go"), Line: 1, Column: 1}}, nil
			},
			wantErr: true,
		},
		// ── Path 6: findTypeSpec returns nil ─────────────────────────────────
		// The file parses successfully but does not contain the requested type.
		{
			name: "path6_findTypeSpec_nil",
			args: map[string]interface{}{"typename": "MissingType"},
			lookupFn: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
				return []analysis.Location{{Path: mismatchFile, Line: 2, Column: 6}}, nil
			},
			wantErr:  false,
			wantText: "Type not found.",
		},
		// ── Path 7: findMethodsInPackage returns error ───────────────────────
		// A canceled context causes checkCancellation to return an error inside
		// the walk function, which filepath.Walk propagates.
		{
			name: "path7_findMethodsInPackage_error",
			args: map[string]interface{}{"typename": "MyStruct"},
			lookupFn: func(ctx context.Context, symbol string, hb chan<- struct{}) ([]analysis.Location, error) {
				return []analysis.Location{{Path: hasTypeFile, Line: 2, Column: 6}}, nil
			},
			ctx:     cancelCtx,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			idx := &mockTypeIndex{}
			if tc.lookupFn != nil {
				idx.LookupFunc = tc.lookupFn
			}

			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}

			m := analysis.NewTypeManager(idx, analysis.NewASTCache("."), &analysistest.MockSecurityProvider{})
			res, err := m.GetTypeInfo(ctx, tc.args, nil)

			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tc.wantErr)
			}

			if tc.wantText != "" && !strings.Contains(res.Text, tc.wantText) {
				t.Errorf("expected result to contain %q, got:\n%s", tc.wantText, res.Text)
			}

			// On error paths, the ToolResult should be zero-valued.
			if tc.wantErr && err != nil {
				if res.Text != "" {
					t.Errorf("expected empty ToolResult on error, got Text=%q", res.Text)
				}
				if res.Metadata != nil || res.BinaryData != nil || res.Error != nil {
					t.Errorf("expected zero-valued ToolResult fields, got Metadata=%v BinaryData=%v Error=%v",
						res.Metadata, res.BinaryData, res.Error)
				}
			}
		})
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

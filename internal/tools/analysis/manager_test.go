package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

func setupAnalysisManager(t *testing.T) (*analysisManager, string) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\ngo 1.25"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package test\nfunc F(){}\nvar _ = F"), 0644); err != nil {
		t.Fatal(err)
	}

	idx, _ := newIndexer(tmpDir)
	cache := newASTCache(".")
	sp := &mockSecurityProvider{}

	m := newAnalysisManager(idx, cache, sp, nil, &mockHealthExecutor{}, persistencetest.NewPlainOSFileSystem())
	return m, tmpDir
}

func assertDelegationSuccess(t *testing.T, res tools.ToolResult, err error, expectedPart string, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed with error: %v", msg, err)
	}
	if !strings.Contains(res.Text, expectedPart) {
		t.Errorf("%s delegation failed: expected part %q not found in %q", msg, expectedPart, res.Text)
	}
}

func testMetricsDelegation(t *testing.T, mgr *analysisManager, ctx context.Context, tmpDir string) {
	res, err := mgr.AnalyzeComplexity(ctx, map[string]interface{}{"path": tmpDir}, nil)
	assertDelegationSuccess(t, res, err, "F - Complexity: 1", "AnalyzeComplexity")
}

func testSymbolDelegation(t *testing.T, mgr *analysisManager, ctx context.Context, tmpDir string) {
	res, err := mgr.ListSymbols(ctx, map[string]interface{}{"path": tmpDir}, nil)
	assertDelegationSuccess(t, res, err, "func F()", "ListSymbols")

	res, err = mgr.FindOrphanedSymbols(ctx, map[string]interface{}{"path": tmpDir}, nil)
	assertDelegationSuccess(t, res, err, "F (Function)", "FindOrphanedSymbols")
}

func testTypeInfoDelegation(t *testing.T, mgr *analysisManager, ctx context.Context) {
	res, err := mgr.GetTypeInfo(ctx, map[string]interface{}{"typename": "NonExistent"}, nil)
	assertDelegationSuccess(t, res, err, "Type not found.", "GetTypeInfo")
}

func testSearchDelegation(t *testing.T, mgr *analysisManager, ctx context.Context, tmpDir string) {
	res, err := mgr.FindUsages(ctx, map[string]interface{}{"path": tmpDir, "query": "F"}, nil)
	assertDelegationSuccess(t, res, err, "test.go", "FindUsages")

	res, err = mgr.FindDefinitions(ctx, map[string]interface{}{"path": tmpDir, "query": "F"}, nil)
	assertDelegationSuccess(t, res, err, "func F()", "FindDefinitions")
}

func testDependencyDelegation(t *testing.T, mgr *analysisManager, ctx context.Context) {
	// These don't have strong assertions in the original test, just checking they don't crash
	_, _ = mgr.SemanticDiff(ctx, map[string]interface{}{"target": "HEAD"}, nil)
	_, _ = mgr.ListImplementations(ctx, map[string]interface{}{"interface_name": "I"}, nil)
	_, _ = mgr.GetPackageGraph(ctx, nil, nil)
}

func testErrorDelegation(t *testing.T, mgr *analysisManager, ctx context.Context) {
	// Passing an invalid type to path should trigger an error in UnmarshalArgs
	_, err := mgr.AnalyzeComplexity(ctx, map[string]interface{}{"path": 123}, nil)
	if err == nil {
		t.Error("Expected error for invalid argument type, got nil")
	}
}

func TestAnalysisManager_Delegation(t *testing.T) {
	t.Parallel()
	mgr, tmpDir := setupAnalysisManager(t)
	ctx := context.Background()

	t.Run("Metrics", func(t *testing.T) {
		t.Parallel()
		testMetricsDelegation(t, mgr, ctx, tmpDir)
	})

	t.Run("Symbols", func(t *testing.T) {
		t.Parallel()
		testSymbolDelegation(t, mgr, ctx, tmpDir)
	})

	t.Run("TypeInfo", func(t *testing.T) {
		t.Parallel()
		testTypeInfoDelegation(t, mgr, ctx)
	})

	t.Run("Search", func(t *testing.T) {
		t.Parallel()
		testSearchDelegation(t, mgr, ctx, tmpDir)
	})

	t.Run("Dependency", func(t *testing.T) {
		t.Parallel()
		testDependencyDelegation(t, mgr, ctx)
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		t.Parallel()
		testErrorDelegation(t, mgr, ctx)
	})
}

func TestAnalysisManager_AnalyzeSequenceFlow(t *testing.T) {
	t.Parallel()
	mgr, _ := setupAnalysisManager(t)
	ctx := context.Background()
	_, _ = mgr.AnalyzeSequenceFlow(ctx, map[string]interface{}{"start_symbol": "F"}, nil)
}

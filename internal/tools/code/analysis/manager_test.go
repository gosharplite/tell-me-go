package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
)

func TestAnalysisManager_Delegation(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\ngo 1.24"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package test\nfunc F(){}"), 0644)

	idx, _ := index.NewIndexer(tmpDir)
	cache := astutil.NewASTCache()
	sp := &mockSecurityProvider{}

	// Real constructor
	m := NewAnalysisManager(idx, cache, sp)

	ctx := context.Background()

	// Test a few delegations
	res, err := m.AnalyzeComplexity(ctx, map[string]interface{}{"path": tmpDir})
	if err != nil || !strings.Contains(res.Text, "F - Complexity: 1") {
		t.Errorf("AnalyzeComplexity delegation failed: %v, %v", err, res.Text)
	}

	res, err = m.ListSymbols(ctx, map[string]interface{}{"path": tmpDir})
	if err != nil || !strings.Contains(res.Text, "func F()") {
		t.Errorf("ListSymbols delegation failed: %v, %v", err, res.Text)
	}

	res, err = m.GetTypeInfo(ctx, map[string]interface{}{"typename": "NonExistent"})
	if err != nil || !strings.Contains(res.Text, "Type not found.") {
		t.Errorf("GetTypeInfo delegation failed: %v, %v", err, res.Text)
	}

	res, err = m.FindUsages(ctx, map[string]interface{}{"path": tmpDir, "query": "F"})
	if err != nil || !strings.Contains(res.Text, "test.go") {
		t.Errorf("FindUsages delegation failed: %v, %v", err, res.Text)
	}

	res, err = m.FindDefinitions(ctx, map[string]interface{}{"path": tmpDir, "query": "F"})
	if err != nil || !strings.Contains(res.Text, "func F()") {
		t.Errorf("FindDefinitions delegation failed: %v, %v", err, res.Text)
	}

	// SemanticDiff and ListImplementations might require more setup or mocks if we were testing their logic here,
	// but we just test delegation.
	_, _ = m.SemanticDiff(ctx, map[string]interface{}{"target": "HEAD"})
	_, _ = m.ListImplementations(ctx, map[string]interface{}{"interface_name": "I"})
	_, _ = m.GetPackageGraph(ctx, nil)
}

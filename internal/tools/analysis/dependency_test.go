package analysis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestDependencyAnalyzer_GetPackageGraph(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	module := "example.com/mod"

	// Create a dummy project structure
	pkg1 := filepath.Join(tmpDir, "pkg1")
	pkg2 := filepath.Join(tmpDir, "pkg2")
	pkg3 := filepath.Join(tmpDir, "pkg3")

	_ = os.MkdirAll(pkg1, 0755)
	_ = os.MkdirAll(pkg2, 0755)
	_ = os.MkdirAll(pkg3, 0755)

	_ = os.WriteFile(filepath.Join(pkg1, "f1.go"), []byte("package pkg1\nimport \"example.com/mod/pkg2\"\nimport \"example.com/mod/pkg3\""), 0644)
	_ = os.WriteFile(filepath.Join(pkg2, "f2.go"), []byte("package pkg2"), 0644)
	_ = os.WriteFile(filepath.Join(pkg3, "f3.go"), []byte("package pkg3\nimport \"example.com/mod/pkg2\""), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/mod"), 0644)

	mockRunner := &mockAnalysisGoRunner{
		getModulePathFunc: func(ctx context.Context) (string, error) {
			return module, nil
		},
		getModuleDirFunc: func(ctx context.Context) (string, error) {
			return tmpDir, nil
		},
	}

	analyzer := newDependencyAnalyzer(mockRunner, &mockSecurityProvider{}, nil, infra_persistence.NewWorkspacePolicy())

	// Set workdir to tmpDir so that go list -m works
	// In a real scenario, the tool would run in the project root.
	// Our mock handles the go list calls.

	res, err := analyzer.GetPackageGraph(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	verifyPackageGraphResults(t, res.Text, module)
}

func verifyPackageGraphResults(t *testing.T, output, module string) {
	if !strings.Contains(output, module+"/pkg1") {
		t.Errorf("Expected pkg1 in output")
	}
	if !strings.Contains(output, "└── "+module+"/pkg2") {
		t.Errorf("Expected pkg2 as dependency")
	}
}

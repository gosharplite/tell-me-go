package analysis

import (
	"context"
	"strings"
	"testing"
)

func TestDependencyAnalyzer_GetPackageGraph(t *testing.T) {
	t.Parallel()
	module := "example.com/mod"
	graphData := module + "/pkg1 -> [" + module + "/pkg2 " + module + "/pkg3]\n" +
		module + "/pkg2 -> []\n" +
		module + "/pkg3 -> [" + module + "/pkg2]"

	mockExec := setupDependencyMock(module, graphData)
	analyzer := newDependencyAnalyzer(mockExec, &mockSecurityProvider{}, nil)

	res, err := analyzer.GetPackageGraph(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	verifyPackageGraphResults(t, res.Text, module)
}

func setupDependencyMock(moduleName, graphOutput string) *mockExecutor {
	return &mockExecutor{
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && len(args) >= 2 && args[0] == "list" && args[1] == "-m" {
				return []byte(moduleName), nil
			}
			return nil, nil
		},
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && len(args) >= 2 && args[0] == "list" && args[1] == "-f" {
				return []byte(graphOutput), nil
			}
			return nil, nil
		},
	}
}

func verifyPackageGraphResults(t *testing.T, output, module string) {
	if !strings.Contains(output, module+"/pkg1") {
		t.Errorf("Expected pkg1 in output")
	}
	if !strings.Contains(output, "└── "+module+"/pkg2") {
		t.Errorf("Expected pkg2 as dependency")
	}
}

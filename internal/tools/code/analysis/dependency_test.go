package analysis

import (
	"context"
	"strings"
	"testing"
)

func TestDependencyAnalyzer_GetPackageGraph(t *testing.T) {
	mockExec := &MockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "list" && args[1] == "-f" {
				return []byte("pkg1 -> [pkg2 pkg3]\npkg2 -> []\npkg3 -> [pkg2]"), nil
			}
			return nil, nil
		},
		OutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "go" && args[0] == "list" && args[1] == "-m" {
				return []byte("example.com/mod"), nil
			}
			return nil, nil
		},
	}

	// Adjust mock to include module prefix for internal filtering
	mockExec.CombinedOutputFunc = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "go" && args[0] == "list" && args[1] == "-f" {
			return []byte("example.com/mod/pkg1 -> [example.com/mod/pkg2 example.com/mod/pkg3]\nexample.com/mod/pkg2 -> []\nexample.com/mod/pkg3 -> [example.com/mod/pkg2]"), nil
		}
		return nil, nil
	}

	analyzer := NewDependencyAnalyzer(mockExec, &mockSecurityProvider{})
	res, err := analyzer.GetPackageGraph(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(res.Text, "example.com/mod/pkg1") {
		t.Errorf("Expected pkg1 in output")
	}
	if !strings.Contains(res.Text, "└── example.com/mod/pkg2") {
		t.Errorf("Expected pkg2 as dependency")
	}
}

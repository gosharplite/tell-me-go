package analysis

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	"golang.org/x/tools/go/packages"
)

var (
	sharedIdx     *indexer
	sharedIdxOnce sync.Once
)

func getSharedIndexer(tb testing.TB) *indexer {
	sharedIdxOnce.Do(func() {
		var err error
		sharedIdx, err = newIndexer(".")
		if err != nil {
			tb.Fatalf("failed to create shared indexer: %v", err)
		}
		if err := sharedIdx.Refresh(context.Background(), nil); err != nil {
			tb.Fatalf("failed to refresh shared indexer: %v", err)
		}
	})
	return sharedIdx
}

type mockSecurityProvider struct{}

func (s *mockSecurityProvider) IsPathSafe(path string) (string, error)     { return path, nil }
func (s *mockSecurityProvider) IsPathWritable(path string) (string, error) { return path, nil }
func (s *mockSecurityProvider) IsCommandAllowed(command string) bool       { return true }
func (s *mockSecurityProvider) IsBypassActive() bool                       { return false }

type mockExecutor struct {
	OutputFunc         func(ctx context.Context, name string, args ...string) ([]byte, error)
	CombinedOutputFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPathFunc       func(file string) (string, error)
}

func (m *mockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.OutputFunc != nil {
		return m.OutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *mockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.CombinedOutputFunc != nil {
		return m.CombinedOutputFunc(ctx, name, args...)
	}
	return nil, nil
}

func (m *mockExecutor) LookPath(file string) (string, error) {
	if m.LookPathFunc != nil {
		return m.LookPathFunc(file)
	}
	return "/usr/bin/" + file, nil
}

type mockIndexer struct {
	symbolIndex
	pkgs  []*packages.Package
	impls map[string][]string
	err   error
}

func (m *mockIndexer) Packages(ctx context.Context, hb chan<- struct{}) ([]*packages.Package, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pkgs, nil
}

func (m *mockIndexer) Refresh(ctx context.Context, hb chan<- struct{}) error {
	return m.err
}

func (m *mockIndexer) GetImplementations(ctx context.Context, id string, hb chan<- struct{}) []string {
	return m.impls[id]
}

func (s *mockSecurityProvider) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

func (s *mockSecurityProvider) LogAudit(action string, args ...any) {}
func (s *mockSecurityProvider) TerminalLock()                       {}
func (s *mockSecurityProvider) TerminalUnlock()                     {}
func (s *mockSecurityProvider) Prompt(message string)               {}
func (s *mockSecurityProvider) Warn(message string)                 {}
func (s *mockSecurityProvider) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (s *mockSecurityProvider) ReadLine(ctx context.Context) (string, error) {
	return "", nil
}

func (s *mockSecurityProvider) Close() error { return nil }

type mockAnalysisGoRunner struct {
	getPackageListFunc       func(ctx context.Context, path string) ([]byte, error)
	getGoDocFunc             func(ctx context.Context, symbol string) ([]byte, error)
	getModulePathFunc        func(ctx context.Context) (string, error)
	getModuleDirFunc         func(ctx context.Context) (string, error)
	runTestsWithCoverageFunc func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	runLinterFunc            func(ctx context.Context) (string, string, error)
}

func (m *mockAnalysisGoRunner) GetPackageList(ctx context.Context, path string) ([]byte, error) {
	if m.getPackageListFunc != nil {
		return m.getPackageListFunc(ctx, path)
	}
	return nil, nil
}

func (m *mockAnalysisGoRunner) GetGoDoc(ctx context.Context, symbol string) ([]byte, error) {
	if m.getGoDocFunc != nil {
		return m.getGoDocFunc(ctx, symbol)
	}
	return nil, nil
}

func (m *mockAnalysisGoRunner) GetModulePath(ctx context.Context) (string, error) {
	if m.getModulePathFunc != nil {
		return m.getModulePathFunc(ctx)
	}
	return "", nil
}

func (m *mockAnalysisGoRunner) GetModuleDir(ctx context.Context) (string, error) {
	if m.getModuleDirFunc != nil {
		return m.getModuleDirFunc(ctx)
	}
	return "", nil
}

func (m *mockAnalysisGoRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
	if m.runTestsWithCoverageFunc != nil {
		return m.runTestsWithCoverageFunc(ctx, path, short, profilePath)
	}
	return toolchain.CoverageReport{}, nil
}

func (m *mockAnalysisGoRunner) RunLinter(ctx context.Context) (string, string, error) {
	if m.runLinterFunc != nil {
		return m.runLinterFunc(ctx)
	}
	return "", "", nil
}

// setupMockGoFile creates a temporary Go file for testing purposes.
// It returns the temporary directory and the absolute path to the Go file.
func setupMockGoFile(t *testing.T, content string) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test_file.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	return tmpDir, path
}

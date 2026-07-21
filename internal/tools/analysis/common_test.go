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
	sharedIdx        *indexer
	sharedFixtureDir string
	sharedIdxOnce    sync.Once
	sharedIdxMu      sync.Mutex
)

const fixtureMod = "example.com/analysis-fixture"

var fixtureFiles = map[string]string{
	"go.mod": "module " + fixtureMod + "\n\ngo 1.25\n",
	"lib/interface.go": `package lib

import "errors"

var ErrAlwaysFails = errors.New("always fails")

type Runner interface {
	Run() error
}

type SimpleRunner struct{}

func (s SimpleRunner) Run() error { return nil }

type FailingRunner struct{}

func (f FailingRunner) Run() error { return ErrAlwaysFails }
`,
	"lib/util.go": `package lib

func Helper(x int) int { return x * 2 }

func internalHelper(s string) string { return "processed: " + s }

type Data struct{ Value string }

func (d Data) Process() string { return "result: " + d.Value }

type unusedType struct{ X int }

func (u unusedType) DoNothing() {}
`,
	"cmd/tool/main.go": `package main

import (
	"fmt"
	"example.com/analysis-fixture/lib"
)

func main() {
	fmt.Println(lib.Helper(5))
	var _ lib.Runner = lib.SimpleRunner{}
}
`,
}

func getFixturePath(tb testing.TB) string {
	tb.Helper()
	dir := filepath.Join(tb.TempDir(), "analysis_fixture")
	for relPath, content := range fixtureFiles {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			tb.Fatalf("create fixture dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			tb.Fatalf("write fixture file: %v", err)
		}
	}
	return dir
}

func getSharedIndexer(tb testing.TB) *indexer {
	sharedIdxOnce.Do(func() {
		sharedFixtureDir = getFixturePath(tb)
	})

	sharedIdxMu.Lock()
	defer sharedIdxMu.Unlock()

	// If fixture was deleted (e.g., by -count=2 cleanup), recreate it.
	if sharedFixtureDir != "" {
		if _, err := os.Stat(sharedFixtureDir); os.IsNotExist(err) {
			sharedFixtureDir = getFixturePath(tb)
			sharedIdx = nil
		}
	}

	if sharedIdx == nil {
		var err error
		sharedIdx, err = newIndexer(sharedFixtureDir)
		if err != nil {
			tb.Fatalf("failed to create shared indexer: %v", err)
		}
		sharedIdx.knownModulePath = fixtureMod
		if err := sharedIdx.Refresh(context.Background(), nil); err != nil {
			tb.Fatalf("failed to refresh shared indexer: %v", err)
		}
	}
	return sharedIdx
}

// getSharedFixtureDir returns the path to the shared analysis fixture directory.
// The directory is created lazily via sync.Once (same as getSharedIndexer).
func getSharedFixtureDir(tb testing.TB) string {
	tb.Helper()
	sharedIdxOnce.Do(func() {
		sharedFixtureDir = getFixturePath(tb)
	})
	sharedIdxMu.Lock()
	defer sharedIdxMu.Unlock()
	return sharedFixtureDir
}

// getRealArchitectureIndexer and findModuleRoot have been moved to
// real_architecture_test.go, which is behind //go:build arch.

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

func (m *mockIndexer) WarmImplementations(ctx context.Context) {}

func (m *mockIndexer) HarvestDeclarations(ctx context.Context, fn func(meta *symMeta) bool, hb chan<- struct{}) error {
	return nil
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

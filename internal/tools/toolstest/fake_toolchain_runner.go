// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// FakeToolchainRunner is a test double for tools.ToolchainRunner. It follows
// the MockExecutor shape: pre-set return values via <Method>Func fields plus
// a call log recording every invocation as the method name, in call order.
// All 12 methods record to the log; a method without its Func set returns
// zero values. This is the canonical tools-family runner double per ADR-021
// locality and ADR-056 canonical-mock-home discipline (issue #1325, ADR-060).
type FakeToolchainRunner struct {
	GetPackageListFunc       func(ctx context.Context, path string) ([]byte, error)
	GetGoDocFunc             func(ctx context.Context, symbol string) ([]byte, error)
	GetModulePathFunc        func(ctx context.Context) (string, error)
	GetModuleDirFunc         func(ctx context.Context) (string, error)
	RunTestsWithCoverageFunc func(ctx context.Context, path string, short bool, profilePath string) (tools.CoverageSummary, error)
	RunLinterFunc            func(ctx context.Context) (string, string, error)
	RunBenchmarksFunc        func(ctx context.Context, path string, benchRegex string) (string, error)
	CheckGovulncheckFunc     func(ctx context.Context) error
	RunModTidyFunc           func(ctx context.Context) ([]byte, error)
	FormatCodeFunc           func(ctx context.Context, path string) ([]byte, error)
	RunTestsFunc             func(ctx context.Context, path string) ([]byte, error)
	BuildCodeFunc            func(ctx context.Context, outputBinary, path string) ([]byte, error)

	// mu guards Calls for concurrent access: the release pipeline
	// (verify_release_readiness) invokes runner methods concurrently, so
	// the call log must be race-safe (issue #1325).
	mu    sync.Mutex
	Calls []string
}

// Called reports whether the named method was invoked at least once.
func (f *FakeToolchainRunner) Called(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		if c == method {
			return true
		}
	}
	return false
}

func (f *FakeToolchainRunner) GetPackageList(ctx context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "GetPackageList")
	f.mu.Unlock()
	if f.GetPackageListFunc != nil {
		return f.GetPackageListFunc(ctx, path)
	}
	return nil, nil
}

func (f *FakeToolchainRunner) GetGoDoc(ctx context.Context, symbol string) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "GetGoDoc")
	f.mu.Unlock()
	if f.GetGoDocFunc != nil {
		return f.GetGoDocFunc(ctx, symbol)
	}
	return nil, nil
}

func (f *FakeToolchainRunner) GetModulePath(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "GetModulePath")
	f.mu.Unlock()
	if f.GetModulePathFunc != nil {
		return f.GetModulePathFunc(ctx)
	}
	return "", nil
}

func (f *FakeToolchainRunner) GetModuleDir(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "GetModuleDir")
	f.mu.Unlock()
	if f.GetModuleDirFunc != nil {
		return f.GetModuleDirFunc(ctx)
	}
	return "", nil
}

func (f *FakeToolchainRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (tools.CoverageSummary, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "RunTestsWithCoverage")
	f.mu.Unlock()
	if f.RunTestsWithCoverageFunc != nil {
		return f.RunTestsWithCoverageFunc(ctx, path, short, profilePath)
	}
	return tools.CoverageSummary{}, nil
}

func (f *FakeToolchainRunner) RunLinter(ctx context.Context) (string, string, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "RunLinter")
	f.mu.Unlock()
	if f.RunLinterFunc != nil {
		return f.RunLinterFunc(ctx)
	}
	return "", "", nil
}

func (f *FakeToolchainRunner) RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "RunBenchmarks")
	f.mu.Unlock()
	if f.RunBenchmarksFunc != nil {
		return f.RunBenchmarksFunc(ctx, path, benchRegex)
	}
	return "", nil
}

func (f *FakeToolchainRunner) CheckGovulncheck(ctx context.Context) error {
	f.mu.Lock()
	f.Calls = append(f.Calls, "CheckGovulncheck")
	f.mu.Unlock()
	if f.CheckGovulncheckFunc != nil {
		return f.CheckGovulncheckFunc(ctx)
	}
	return nil
}

func (f *FakeToolchainRunner) RunModTidy(ctx context.Context) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "RunModTidy")
	f.mu.Unlock()
	if f.RunModTidyFunc != nil {
		return f.RunModTidyFunc(ctx)
	}
	return nil, nil
}

func (f *FakeToolchainRunner) FormatCode(ctx context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "FormatCode")
	f.mu.Unlock()
	if f.FormatCodeFunc != nil {
		return f.FormatCodeFunc(ctx, path)
	}
	return nil, nil
}

func (f *FakeToolchainRunner) RunTests(ctx context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "RunTests")
	f.mu.Unlock()
	if f.RunTestsFunc != nil {
		return f.RunTestsFunc(ctx, path)
	}
	return nil, nil
}

func (f *FakeToolchainRunner) BuildCode(ctx context.Context, outputBinary, path string) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, "BuildCode")
	f.mu.Unlock()
	if f.BuildCodeFunc != nil {
		return f.BuildCodeFunc(ctx, outputBinary, path)
	}
	return nil, nil
}

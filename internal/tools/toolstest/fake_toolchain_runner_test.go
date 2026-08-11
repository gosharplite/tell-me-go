// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Compile-time assertion: FakeToolchainRunner implements the full 12-method
// tools.ToolchainRunner port (issue #1325, ADR-060).
var _ tools.ToolchainRunner = (*FakeToolchainRunner)(nil)

// assertCallLog verifies the method was recorded in the Calls log and is the
// most recent entry. This is the fake's identity assertion: a probe that
// unexpectedly reaches the wrong method fails here with the recorded call in
// hand.
func assertCallLog(t *testing.T, f *FakeToolchainRunner, method string) {
	t.Helper()
	if !f.Called(method) {
		t.Errorf("Called(%q) = false; want true", method)
	}
	if len(f.Calls) == 0 {
		t.Fatalf("Calls is empty; want last element %q", method)
	}
	if got := f.Calls[len(f.Calls)-1]; got != method {
		t.Errorf("last Call = %q; want %q", got, method)
	}
}

func TestFakeToolchainRunner_PresetValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(t *testing.T, f *FakeToolchainRunner)
	}{
		{
			name: "GetPackageList",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.GetPackageListFunc = func(ctx context.Context, path string) ([]byte, error) {
					return []byte("pkgs"), nil
				}
				got, err := f.GetPackageList(ctx, ".")
				if err != nil {
					t.Fatalf("GetPackageList() returned error: %v", err)
				}
				if string(got) != "pkgs" {
					t.Errorf("GetPackageList() = %q; want %q", got, "pkgs")
				}
				assertCallLog(t, f, "GetPackageList")
			},
		},
		{
			name: "GetGoDoc",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.GetGoDocFunc = func(ctx context.Context, symbol string) ([]byte, error) {
					return []byte("doc"), nil
				}
				got, err := f.GetGoDoc(ctx, "fmt.Println")
				if err != nil {
					t.Fatalf("GetGoDoc() returned error: %v", err)
				}
				if string(got) != "doc" {
					t.Errorf("GetGoDoc() = %q; want %q", got, "doc")
				}
				assertCallLog(t, f, "GetGoDoc")
			},
		},
		{
			name: "GetModulePath",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.GetModulePathFunc = func(ctx context.Context) (string, error) {
					return "module/path", nil
				}
				got, err := f.GetModulePath(ctx)
				if err != nil {
					t.Fatalf("GetModulePath() returned error: %v", err)
				}
				if got != "module/path" {
					t.Errorf("GetModulePath() = %q; want %q", got, "module/path")
				}
				assertCallLog(t, f, "GetModulePath")
			},
		},
		{
			name: "GetModuleDir",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.GetModuleDirFunc = func(ctx context.Context) (string, error) {
					return "/module/dir", nil
				}
				got, err := f.GetModuleDir(ctx)
				if err != nil {
					t.Fatalf("GetModuleDir() returned error: %v", err)
				}
				if got != "/module/dir" {
					t.Errorf("GetModuleDir() = %q; want %q", got, "/module/dir")
				}
				assertCallLog(t, f, "GetModuleDir")
			},
		},
		{
			name: "RunTestsWithCoverage",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.RunTestsWithCoverageFunc = func(ctx context.Context, path string, short bool, profilePath string) (tools.CoverageSummary, error) {
					return tools.CoverageSummary{PassedCount: 3, NoGoFiles: true, CoveragePct: "85.0%", SummaryOutput: "total:"}, nil
				}
				want := tools.CoverageSummary{PassedCount: 3, NoGoFiles: true, CoveragePct: "85.0%", SummaryOutput: "total:"}
				got, err := f.RunTestsWithCoverage(ctx, ".", false, "coverage.out")
				if err != nil {
					t.Fatalf("RunTestsWithCoverage() returned error: %v", err)
				}
				if got != want {
					t.Errorf("RunTestsWithCoverage() = %+v; want %+v", got, want)
				}
				assertCallLog(t, f, "RunTestsWithCoverage")
			},
		},
		{
			name: "RunLinter",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.RunLinterFunc = func(ctx context.Context) (string, string, error) {
					return "out", "golangci-lint", errors.New("lint err")
				}
				out, name, err := f.RunLinter(ctx)
				if out != "out" {
					t.Errorf("RunLinter() output = %q; want %q", out, "out")
				}
				if name != "golangci-lint" {
					t.Errorf("RunLinter() name = %q; want %q", name, "golangci-lint")
				}
				if err == nil || err.Error() != "lint err" {
					t.Errorf("RunLinter() err = %v; want %q", err, "lint err")
				}
				assertCallLog(t, f, "RunLinter")
			},
		},
		{
			name: "RunBenchmarks",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.RunBenchmarksFunc = func(ctx context.Context, path string, benchRegex string) (string, error) {
					return "bench", nil
				}
				got, err := f.RunBenchmarks(ctx, ".", "Benchmark.*")
				if err != nil {
					t.Fatalf("RunBenchmarks() returned error: %v", err)
				}
				if got != "bench" {
					t.Errorf("RunBenchmarks() = %q; want %q", got, "bench")
				}
				assertCallLog(t, f, "RunBenchmarks")
			},
		},
		{
			name: "CheckGovulncheck",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.CheckGovulncheckFunc = func(ctx context.Context) error {
					return errors.New("not installed")
				}
				err := f.CheckGovulncheck(ctx)
				if err == nil || err.Error() != "not installed" {
					t.Errorf("CheckGovulncheck() err = %v; want %q", err, "not installed")
				}
				assertCallLog(t, f, "CheckGovulncheck")
			},
		},
		{
			name: "RunModTidy",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.RunModTidyFunc = func(ctx context.Context) ([]byte, error) {
					return []byte("tidy"), nil
				}
				got, err := f.RunModTidy(ctx)
				if err != nil {
					t.Fatalf("RunModTidy() returned error: %v", err)
				}
				if string(got) != "tidy" {
					t.Errorf("RunModTidy() = %q; want %q", got, "tidy")
				}
				assertCallLog(t, f, "RunModTidy")
			},
		},
		{
			name: "FormatCode",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.FormatCodeFunc = func(ctx context.Context, path string) ([]byte, error) {
					return []byte("fmt"), nil
				}
				got, err := f.FormatCode(ctx, ".")
				if err != nil {
					t.Fatalf("FormatCode() returned error: %v", err)
				}
				if string(got) != "fmt" {
					t.Errorf("FormatCode() = %q; want %q", got, "fmt")
				}
				assertCallLog(t, f, "FormatCode")
			},
		},
		{
			name: "RunTests",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.RunTestsFunc = func(ctx context.Context, path string) ([]byte, error) {
					return []byte("PASS"), nil
				}
				got, err := f.RunTests(ctx, "./internal/tools/...")
				if err != nil {
					t.Fatalf("RunTests() returned error: %v", err)
				}
				if string(got) != "PASS" {
					t.Errorf("RunTests() = %q; want %q", got, "PASS")
				}
				assertCallLog(t, f, "RunTests")
			},
		},
		{
			name: "BuildCode",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				f.BuildCodeFunc = func(ctx context.Context, outputBinary, path string) ([]byte, error) {
					return []byte("built"), nil
				}
				got, err := f.BuildCode(ctx, "/tmp/bin", ".")
				if err != nil {
					t.Fatalf("BuildCode() returned error: %v", err)
				}
				if string(got) != "built" {
					t.Errorf("BuildCode() = %q; want %q", got, "built")
				}
				assertCallLog(t, f, "BuildCode")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &FakeToolchainRunner{}
			tt.run(t, f)
		})
	}
}

func TestFakeToolchainRunner_ZeroDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(t *testing.T, f *FakeToolchainRunner)
	}{
		{
			name: "GetPackageList",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.GetPackageList(ctx, ".")
				if got != nil || err != nil {
					t.Errorf("GetPackageList() = (%v, %v); want (nil, nil)", got, err)
				}
				if !f.Called("GetPackageList") {
					t.Error("GetPackageList not recorded in Calls")
				}
			},
		},
		{
			name: "GetGoDoc",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.GetGoDoc(ctx, "fmt.Println")
				if got != nil || err != nil {
					t.Errorf("GetGoDoc() = (%v, %v); want (nil, nil)", got, err)
				}
				if !f.Called("GetGoDoc") {
					t.Error("GetGoDoc not recorded in Calls")
				}
			},
		},
		{
			name: "GetModulePath",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.GetModulePath(ctx)
				if got != "" || err != nil {
					t.Errorf("GetModulePath() = (%q, %v); want (\"\", nil)", got, err)
				}
				if !f.Called("GetModulePath") {
					t.Error("GetModulePath not recorded in Calls")
				}
			},
		},
		{
			name: "GetModuleDir",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.GetModuleDir(ctx)
				if got != "" || err != nil {
					t.Errorf("GetModuleDir() = (%q, %v); want (\"\", nil)", got, err)
				}
				if !f.Called("GetModuleDir") {
					t.Error("GetModuleDir not recorded in Calls")
				}
			},
		},
		{
			name: "RunTestsWithCoverage",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.RunTestsWithCoverage(ctx, ".", false, "coverage.out")
				if got != (tools.CoverageSummary{}) || err != nil {
					t.Errorf("RunTestsWithCoverage() = (%+v, %v); want (%+v, nil)", got, err, tools.CoverageSummary{})
				}
				if !f.Called("RunTestsWithCoverage") {
					t.Error("RunTestsWithCoverage not recorded in Calls")
				}
			},
		},
		{
			name: "RunLinter",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				out, name, err := f.RunLinter(ctx)
				if out != "" || name != "" || err != nil {
					t.Errorf("RunLinter() = (%q, %q, %v); want (\"\", \"\", nil)", out, name, err)
				}
				if !f.Called("RunLinter") {
					t.Error("RunLinter not recorded in Calls")
				}
			},
		},
		{
			name: "RunBenchmarks",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.RunBenchmarks(ctx, ".", "Benchmark.*")
				if got != "" || err != nil {
					t.Errorf("RunBenchmarks() = (%q, %v); want (\"\", nil)", got, err)
				}
				if !f.Called("RunBenchmarks") {
					t.Error("RunBenchmarks not recorded in Calls")
				}
			},
		},
		{
			name: "CheckGovulncheck",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				err := f.CheckGovulncheck(ctx)
				if err != nil {
					t.Errorf("CheckGovulncheck() = %v; want nil", err)
				}
				if !f.Called("CheckGovulncheck") {
					t.Error("CheckGovulncheck not recorded in Calls")
				}
			},
		},
		{
			name: "RunModTidy",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.RunModTidy(ctx)
				if got != nil || err != nil {
					t.Errorf("RunModTidy() = (%v, %v); want (nil, nil)", got, err)
				}
				if !f.Called("RunModTidy") {
					t.Error("RunModTidy not recorded in Calls")
				}
			},
		},
		{
			name: "FormatCode",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.FormatCode(ctx, ".")
				if got != nil || err != nil {
					t.Errorf("FormatCode() = (%v, %v); want (nil, nil)", got, err)
				}
				if !f.Called("FormatCode") {
					t.Error("FormatCode not recorded in Calls")
				}
			},
		},
		{
			name: "RunTests",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.RunTests(ctx, "./internal/tools/...")
				if got != nil || err != nil {
					t.Errorf("RunTests() = (%v, %v); want (nil, nil)", got, err)
				}
				if !f.Called("RunTests") {
					t.Error("RunTests not recorded in Calls")
				}
			},
		},
		{
			name: "BuildCode",
			run: func(t *testing.T, f *FakeToolchainRunner) {
				got, err := f.BuildCode(ctx, "/tmp/bin", ".")
				if got != nil || err != nil {
					t.Errorf("BuildCode() = (%v, %v); want (nil, nil)", got, err)
				}
				if !f.Called("BuildCode") {
					t.Error("BuildCode not recorded in Calls")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &FakeToolchainRunner{}
			tt.run(t, f)
		})
	}
}

func TestFakeToolchainRunner_CallOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := &FakeToolchainRunner{}

	if _, err := f.RunTestsWithCoverage(ctx, ".", false, "coverage.out"); err != nil {
		t.Fatalf("RunTestsWithCoverage() returned error: %v", err)
	}
	if _, _, err := f.RunLinter(ctx); err != nil {
		t.Fatalf("RunLinter() returned error: %v", err)
	}
	if _, err := f.GetGoDoc(ctx, "fmt.Println"); err != nil {
		t.Fatalf("GetGoDoc() returned error: %v", err)
	}

	want := []string{"RunTestsWithCoverage", "RunLinter", "GetGoDoc"}
	if len(f.Calls) != len(want) {
		t.Fatalf("len(Calls) = %d; want %d", len(f.Calls), len(want))
	}
	for i, c := range f.Calls {
		if c != want[i] {
			t.Errorf("Calls[%d] = %q; want %q", i, c, want[i])
		}
	}
}

func TestFakeToolchainRunner_ConcurrentAppends(t *testing.T) {
	f := &FakeToolchainRunner{}
	const goroutines = 16
	const callsPerGoroutine = 50
	var wg sync.WaitGroup
	ctx := context.Background()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				switch (seed + i) % 3 {
				case 0:
					_, _, _ = f.RunLinter(ctx)
				case 1:
					_, _ = f.RunTests(ctx, "./...")
				case 2:
					_, _ = f.BuildCode(ctx, "/tmp/out", "./cmd/tell-me-go")
				}
			}
		}(g)
	}
	wg.Wait()
	// All goroutines joined: reading Calls is now safe.
	if len(f.Calls) != goroutines*callsPerGoroutine {
		t.Errorf("Calls length = %d; want %d", len(f.Calls), goroutines*callsPerGoroutine)
	}
	for _, method := range []string{"RunLinter", "RunTests", "BuildCode"} {
		if !f.Called(method) {
			t.Errorf("Called(%q) = false; want true after concurrent appends", method)
		}
	}
}

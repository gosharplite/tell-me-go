// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/toolchain"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

type mockDeadCodeAnalyzer struct {
	reports []orphanReport
	err     error
}

func (m *mockDeadCodeAnalyzer) GatherOrphanReports(ctx context.Context, path string, deep bool, hb chan<- struct{}) ([]orphanReport, error) {
	return m.reports, m.err
}

func (m *mockDeadCodeAnalyzer) FindOrphanedSymbols(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

type mockGoRunner struct {
	runTestsWithCoverageFunc func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
	runBenchmarksFunc        func(ctx context.Context, path string, benchRegex string) (string, error)
	runLinterFunc            func(ctx context.Context) (string, string, error)
	runModTidyFunc           func(ctx context.Context) ([]byte, error)
	formatCodeFunc           func(ctx context.Context, path string) ([]byte, error)
	getPackageListFunc       func(ctx context.Context, path string) ([]byte, error)
	getGoDocFunc             func(ctx context.Context, symbol string) ([]byte, error)
	checkGovulncheckFunc     func(ctx context.Context) error
	getModulePathFunc        func(ctx context.Context) (string, error)
	getModuleDirFunc         func(ctx context.Context) (string, error)
}

func (m *mockGoRunner) RunTestsWithCoverage(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
	if m.runTestsWithCoverageFunc != nil {
		return m.runTestsWithCoverageFunc(ctx, path, short, profilePath)
	}
	return toolchain.CoverageReport{
		PassedCount:   2,
		CoveragePct:   "82.5%",
		TestOutput:    "ok package1\nok package2",
		SummaryOutput: "total: (statements) 82.5%",
	}, nil
}

func (m *mockGoRunner) RunBenchmarks(ctx context.Context, path string, benchRegex string) (string, error) {
	if m.runBenchmarksFunc != nil {
		return m.runBenchmarksFunc(ctx, path, benchRegex)
	}
	return "", nil
}

func (m *mockGoRunner) RunLinter(ctx context.Context) (string, string, error) {
	if m.runLinterFunc != nil {
		return m.runLinterFunc(ctx)
	}
	return "", "golangci-lint", nil
}

func (m *mockGoRunner) RunModTidy(ctx context.Context) ([]byte, error) {
	if m.runModTidyFunc != nil {
		return m.runModTidyFunc(ctx)
	}
	return []byte("success"), nil
}

func (m *mockGoRunner) FormatCode(ctx context.Context, path string) ([]byte, error) {
	if m.formatCodeFunc != nil {
		return m.formatCodeFunc(ctx, path)
	}
	return []byte("success"), nil
}

func (m *mockGoRunner) GetPackageList(ctx context.Context, path string) ([]byte, error) {
	if m.getPackageListFunc != nil {
		return m.getPackageListFunc(ctx, path)
	}
	return nil, nil
}

func (m *mockGoRunner) GetGoDoc(ctx context.Context, symbol string) ([]byte, error) {
	if m.getGoDocFunc != nil {
		return m.getGoDocFunc(ctx, symbol)
	}
	return nil, nil
}

func (m *mockGoRunner) CheckGovulncheck(ctx context.Context) error {
	if m.checkGovulncheckFunc != nil {
		return m.checkGovulncheckFunc(ctx)
	}
	return nil
}

func (m *mockGoRunner) GetModulePath(ctx context.Context) (string, error) {
	if m.getModulePathFunc != nil {
		return m.getModulePathFunc(ctx)
	}
	return "github.com/gosharplite/tell-me-go", nil
}

func (m *mockGoRunner) GetModuleDir(ctx context.Context) (string, error) {
	if m.getModuleDirFunc != nil {
		return m.getModuleDirFunc(ctx)
	}
	return ".", nil
}

type mockHealthExecutor struct{}

func (m *mockHealthExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.CombinedOutput(ctx, name, args...)
}

func (m *mockHealthExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "go" && len(args) > 0 && args[0] == "test" {
		return []byte("ok package1\nok package2"), nil
	}
	if name == "go" && len(args) > 1 && args[0] == "tool" && args[1] == "cover" {
		return []byte("total: (statements) 82.5%"), nil
	}
	if name == "golangci-lint" || name == "staticcheck" {
		return []byte(""), nil
	}
	return []byte(""), nil
}

func TestHealthManager_GetCodeHealth(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	idx, _ := newIndexer(".")
	cache := newASTCache(".")
	mockExec := &mockHealthExecutor{}
	mockRunner := &mockGoRunner{}
	mockDead := &mockDeadCodeAnalyzer{}
	ana := newAnalysisManager(idx, cache, sm, nil, mockExec, persistencetest.NewPlainOSFileSystem(), infra_persistence.NewWorkspacePolicy(), mockDead)
	hea := &healthManager{SP: sm, complexity: ana.complexity, deadCode: mockDead, Exec: mockExec, Runner: mockRunner}

	ctx := context.Background()
	res, err := hea.GetCodeHealth(ctx, nil, nil)
	if err != nil {
		t.Fatalf("GetCodeHealth failed: %v", err)
	}

	if !strings.Contains(res.Text, "Project Health Dashboard") {
		t.Errorf("expected dashboard title, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "| Metric | Status | Details |") {
		t.Errorf("expected table header, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "| **Dead Code** |") {
		t.Errorf("expected Dead Code row, got %q", res.Text)
	}
	if strings.Contains(res.Text, "| **Security** |") {
		t.Errorf("did not expect Security row, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "82.5%") {
		t.Errorf("expected 82.5%% coverage, got %q", res.Text)
	}
}

func TestHealthManager_GetCodeHealth_Cancelled(t *testing.T) {
	t.Parallel()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	idx, _ := newIndexer(".")
	cache := newASTCache(".")
	mockExec := &mockHealthExecutor{}
	mockRunner := &mockGoRunner{}
	ana := newAnalysisManager(idx, cache, sm, nil, mockExec, persistencetest.NewPlainOSFileSystem(), infra_persistence.NewWorkspacePolicy(), &mockDeadCodeAnalyzer{})
	hea := &healthManager{SP: sm, complexity: ana.complexity, deadCode: &mockDeadCodeAnalyzer{}, Exec: mockExec, Runner: mockRunner}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	res, err := hea.GetCodeHealth(ctx, nil, nil)
	if err != nil {
		t.Fatalf("GetCodeHealth failed: %v", err)
	}

	if !strings.Contains(res.Text, "Operation cancelled") {
		t.Errorf("expected cancellation message, got %q", res.Text)
	}
}

func TestHealthManager_GenerateRecommendation(t *testing.T) {
	t.Parallel()
	hea := &healthManager{}

	tests := []struct {
		name string
		test string
		cov  string
		lint string
		comp string
		dead string
		want []string
	}{
		{
			name: "excellent health",
			test: "PASS",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			dead: "CLEAN",
			want: []string{"Project health is excellent."},
		},
		{
			name: "failing tests",
			test: "FAIL",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			dead: "CLEAN",
			want: []string{"Fix failing or timed-out tests immediately."},
		},
		{
			name: "low coverage",
			test: "PASS",
			cov:  "70.0%",
			lint: "CLEAN",
			comp: "GOOD",
			dead: "CLEAN",
			want: []string{"Coverage (70.0%) is below the 80% target."},
		},
		{
			name: "complexity and linting",
			test: "PASS",
			cov:  "85.0%",
			lint: "5 Issues",
			comp: "3 Alerts",
			dead: "CLEAN",
			want: []string{"Refactor high-complexity functions.", "Address linting issues."},
		},
		{
			name: "dead code issues",
			test: "PASS",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			dead: "2 Issues",
			want: []string{"Remove dead or effectively private code to improve encapsulation."},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hea.generateRecommendation(tt.test, tt.cov, tt.lint, tt.comp, tt.dead)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("generateRecommendation() = %q, want it to contain %q", got, w)
				}
			}
		})
	}
}

func TestHealthManager_CheckDeadCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mockReports []orphanReport
		mockErr     error
		wantStatus  string
		wantDetails string
	}{
		{
			name:        "error path",
			mockErr:     fmt.Errorf("analyzer failure"),
			wantStatus:  "ERROR",
			wantDetails: "analyzer failure",
		},
		{
			name:        "clean path",
			mockReports: nil,
			wantStatus:  "CLEAN",
			wantDetails: "No orphaned symbols found",
		},
		{
			name: "mixed issues",
			mockReports: []orphanReport{
				{Severity: "DEAD"},
				{Severity: "PRIVATE"},
				{Severity: "PRIVATE"},
			},
			wantStatus:  "3 Issues",
			wantDetails: "1 DEAD, 2 PRIVATE",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Setup mock
			mockAna := &mockDeadCodeAnalyzer{reports: tt.mockReports, err: tt.mockErr}

			// Inject into a dummy healthManager
			hea := &healthManager{
				deadCode: mockAna,
			}

			status, details := hea.checkDeadCode(context.Background(), nil)
			if status != tt.wantStatus {
				t.Errorf("got status %q, want %q", status, tt.wantStatus)
			}
			if details != tt.wantDetails {
				t.Errorf("got details %q, want %q", details, tt.wantDetails)
			}
		})
	}
}

type coverageMockExecutor struct {
	t *testing.T
}

func (m *coverageMockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "go" && len(args) > 1 && args[0] == "list" && args[1] == "-m" {
		return []byte("github.com/gosharplite/tell-me-go"), nil
	}
	return []byte(""), nil
}

func (m *coverageMockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "go" && len(args) > 0 && args[0] == "test" {
		for _, arg := range args {
			if strings.HasPrefix(arg, "-coverprofile=") {
				path := strings.TrimPrefix(arg, "-coverprofile=")
				content := "mode: set\ngithub.com/gosharplite/tell-me-go/internal/domain/events/events.go:1.1,2.1 1 0\n"
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					m.t.Errorf("failed to write mock coverage file: %v", err)
				}
			}
		}
		return []byte("ok"), nil
	}
	return []byte(""), nil
}

func TestHealthManager_GetDetailedCoverage(t *testing.T) {
	t.Parallel()
	mockExec := &coverageMockExecutor{t: t}
	mockRunner := &mockGoRunner{
		runTestsWithCoverageFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
			content := "mode: set\ngithub.com/gosharplite/tell-me-go/internal/domain/events/events.go:1.1,2.1 1 0\n"
			if err := os.WriteFile(profilePath, []byte(content), 0644); err != nil {
				t.Errorf("failed to write mock coverage file: %v", err)
			}
			return toolchain.CoverageReport{}, nil
		},
	}
	hea := &healthManager{Exec: mockExec, Runner: mockRunner}

	ctx := context.Background()
	args := map[string]interface{}{"path": "./internal/domain/events/..."}
	res, err := hea.GetDetailedCoverage(ctx, args, nil)
	if err != nil {
		t.Fatalf("GetDetailedCoverage failed: %v", err)
	}

	if !strings.Contains(res.Text, "Detailed Coverage Report") {
		t.Errorf("expected report title, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "events.go") {
		t.Errorf("expected file name in report, got %q", res.Text)
	}
}

func (m *mockHealthExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

func (m *coverageMockExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}

// errorComplexityAnalyzer implements complexityAnalyzer and always returns
// an error from GatherComplexities to exercise the error path in checkComplexity.
type errorComplexityAnalyzer struct{}

func (e *errorComplexityAnalyzer) Analyze(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (e *errorComplexityAnalyzer) GatherComplexities(ctx context.Context, root string, hb chan<- struct{}) ([]funcComplexity, []string, error) {
	return nil, nil, fmt.Errorf("complexity scan failed")
}

func TestCheckComplexity_ErrorPath(t *testing.T) {
	t.Parallel()
	m := &healthManager{
		complexity: &errorComplexityAnalyzer{},
	}
	status, details, alerts := m.checkComplexity(context.Background(), nil)
	if status != "ERROR" {
		t.Errorf("expected ERROR status, got %q", status)
	}
	if !strings.Contains(details, "complexity scan failed") {
		t.Errorf("expected error details, got %q", details)
	}
	if alerts != nil {
		t.Errorf("expected nil alerts on error, got %v", alerts)
	}
}

func TestRecommendCoverage_EdgeCases(t *testing.T) {
	t.Parallel()
	m := &healthManager{}
	tests := []struct {
		name string
		cov  string
		want string
	}{
		{"ERROR status", "ERROR", "Address issues preventing coverage analysis."},
		{"TIMEOUT status", "TIMEOUT", "Address issues preventing coverage analysis."},
		{"below threshold", "75.5%", "Coverage (75.5%) is below the 80% target."},
		{"at threshold", "80.0%", ""},
		{"above threshold", "92.0%", ""},
		{"N/A", "N/A", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := m.recommendCoverage(tt.cov)
			if got != tt.want {
				t.Errorf("recommendCoverage(%q) = %q, want %q", tt.cov, got, tt.want)
			}
		})
	}
}

func TestGetDetailedCoverage_DefaultPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockExecutor{
		CombinedOutputFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, fmt.Errorf("no go files")
		},
	}
	m := &healthManager{Exec: mock, Runner: toolchain.NewGoRunner(mock)}

	// No "path" key → defaults to "./..."
	result, err := m.GetDetailedCoverage(ctx, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Text, "Error:") {
		t.Errorf("expected error text in result, got: %s", result.Text)
	}
}

func TestRunTestsAndCoverage_ErrorPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		ctxFunc          func() (context.Context, func())
		runnerFunc       func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error)
		wantTestStatus   string
		wantTestContains string
		wantCovStatus    string
		wantCovContains  string
	}{
		{
			name: "deadline exceeded",
			ctxFunc: func() (context.Context, func()) {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Hour))
				return ctx, cancel
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{}, context.DeadlineExceeded
			},
			wantTestStatus:   "TIMEOUT",
			wantTestContains: "timed out",
			wantCovStatus:    "ERROR",
			wantCovContains:  "Failed to generate coverage summary",
		},
		{
			name: "context canceled",
			ctxFunc: func() (context.Context, func()) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // ctx.Err() returns context.Canceled
				return ctx, cancel
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{}, context.Canceled
			},
			wantTestStatus:   "CANCELLED",
			wantTestContains: "cancelled",
			wantCovStatus:    "ERROR",
			wantCovContains:  "Failed to generate coverage summary",
		},
		{
			name: "generic test failure",
			ctxFunc: func() (context.Context, func()) {
				return context.Background(), func() {}
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{}, errors.New("exit status 1")
			},
			wantTestStatus:   "FAIL",
			wantTestContains: "failed",
			wantCovStatus:    "ERROR",
			wantCovContains:  "Failed to generate coverage summary",
		},
		{
			name: "coverage N/A",
			ctxFunc: func() (context.Context, func()) {
				return context.Background(), func() {}
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{CoveragePct: "N/A", PassedCount: 3}, nil
			},
			wantTestStatus:   "PASS",
			wantTestContains: "3 packages passed",
			wantCovStatus:    "N/A",
			wantCovContains:  "Could not parse coverage",
		},
		{
			name: "no Go files",
			ctxFunc: func() (context.Context, func()) {
				return context.Background(), func() {}
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{NoGoFiles: true, PassedCount: 0}, nil
			},
			wantTestStatus:   "PASS",
			wantTestContains: "0 packages passed",
			wantCovStatus:    "ERROR",
			wantCovContains:  "Failed to generate coverage summary",
		},
		{
			name: "empty coverage",
			ctxFunc: func() (context.Context, func()) {
				return context.Background(), func() {}
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{CoveragePct: "", PassedCount: 1}, nil
			},
			wantTestStatus:   "PASS",
			wantTestContains: "1 packages passed",
			wantCovStatus:    "ERROR",
			wantCovContains:  "Failed to generate coverage summary",
		},
		{
			name: "coverage success",
			ctxFunc: func() (context.Context, func()) {
				return context.Background(), func() {}
			},
			runnerFunc: func(ctx context.Context, path string, short bool, profilePath string) (toolchain.CoverageReport, error) {
				return toolchain.CoverageReport{CoveragePct: "85.0%", PassedCount: 5}, nil
			},
			wantTestStatus:   "PASS",
			wantTestContains: "5 packages passed",
			wantCovStatus:    "85.0%",
			wantCovContains:  "Target: > 80%",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &healthManager{
				Runner: &mockGoRunner{
					runTestsWithCoverageFunc: tt.runnerFunc,
				},
			}
			ctx, cleanup := tt.ctxFunc()
			defer cleanup()
			tStatus, tDetails, cStatus, cDetails := m.runTestsAndCoverage(ctx)
			if tt.wantTestStatus != "" && tStatus != tt.wantTestStatus {
				t.Errorf("test status: got %q, want %q", tStatus, tt.wantTestStatus)
			}
			if tt.wantTestContains != "" && !strings.Contains(strings.ToLower(tDetails), strings.ToLower(tt.wantTestContains)) {
				t.Errorf("test details: got %q, want containing %q", tDetails, tt.wantTestContains)
			}
			if tt.wantCovStatus != "" && cStatus != tt.wantCovStatus {
				t.Errorf("coverage status: got %q, want %q", cStatus, tt.wantCovStatus)
			}
			if tt.wantCovContains != "" && !strings.Contains(strings.ToLower(cDetails), strings.ToLower(tt.wantCovContains)) {
				t.Errorf("coverage details: got %q, want containing %q", cDetails, tt.wantCovContains)
			}
		})
	}
}

func TestRunLint_ErrorPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		runLinterFn  func(ctx context.Context) (string, string, error)
		wantStatus   string
		wantContains string
	}{
		{
			name: "no linter found",
			runLinterFn: func(ctx context.Context) (string, string, error) {
				return "", "", toolchain.ErrNoSupportedLinter
			},
			wantStatus:   "SKIP",
			wantContains: "No linter found",
		},
		{
			name: "generic error",
			runLinterFn: func(ctx context.Context) (string, string, error) {
				return "", "", errors.New("binary not found")
			},
			wantStatus:   "ERROR",
			wantContains: "binary not found",
		},
		{
			name: "exit error with issues",
			runLinterFn: func(ctx context.Context) (string, string, error) {
				return "file.go:10:5: warning", "staticcheck", &exec.ExitError{}
			},
			wantStatus:   "1 Issues",
			wantContains: "Using staticcheck",
		},
		{
			name: "exit error but no output",
			runLinterFn: func(ctx context.Context) (string, string, error) {
				return "", "golangci-lint", &exec.ExitError{}
			},
			wantStatus:   "CLEAN",
			wantContains: "All checks passed",
		},
		{
			name: "no issues no error",
			runLinterFn: func(ctx context.Context) (string, string, error) {
				return "", "golangci-lint", nil
			},
			wantStatus:   "CLEAN",
			wantContains: "All checks passed",
		},
		{
			name: "output but not issue lines",
			runLinterFn: func(ctx context.Context) (string, string, error) {
				return "just a warning message\nno colon numbers", "staticcheck", &exec.ExitError{}
			},
			wantStatus:   "CLEAN",
			wantContains: "All checks passed",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &healthManager{
				Runner: &mockGoRunner{
					runLinterFunc: tt.runLinterFn,
				},
			}
			status, details := m.runLint(context.Background())
			if status != tt.wantStatus {
				t.Errorf("status: got %q, want %q", status, tt.wantStatus)
			}
			if !strings.Contains(details, tt.wantContains) {
				t.Errorf("details: got %q, want containing %q", details, tt.wantContains)
			}
		})
	}
}

func TestHealthStartHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("nil hb, already-done channel", func(t *testing.T) {
		t.Parallel()
		m := &healthManager{}
		done := make(chan struct{})
		close(done)
		m.startHeartbeat(done, nil) // must return immediately, no panic
	})

	t.Run("nil hb, open done channel then close", func(t *testing.T) {
		t.Parallel()
		m := &healthManager{}
		done := make(chan struct{})
		go func() {
			m.startHeartbeat(done, nil)
		}()
		time.Sleep(10 * time.Millisecond)
		close(done)
	})

	t.Run("buffered hb, receives heartbeat", func(t *testing.T) {
		t.Parallel()
		m := &healthManager{}
		done := make(chan struct{})
		hb := make(chan struct{}, 2)
		go func() {
			m.startHeartbeat(done, hb)
		}()
		select {
		case <-hb:
			// received
		case <-time.After(3 * time.Second):
			t.Error("timed out waiting for heartbeat")
		}
		close(done)
	})

	t.Run("hb full, does not block", func(t *testing.T) {
		t.Parallel()
		m := &healthManager{}
		done := make(chan struct{})
		hb := make(chan struct{}, 1)
		hb <- struct{}{} // pre-fill buffer
		go func() {
			m.startHeartbeat(done, hb)
		}()
		time.Sleep(10 * time.Millisecond)
		close(done)
		// test passes if we reach here without deadlock/panic
	})
}

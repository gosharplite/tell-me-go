// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/service/toolchain"
)

type mockDeadCodeAnalyzer struct {
	reports []orphanReport
	err     error
}

func (m *mockDeadCodeAnalyzer) GatherOrphanReports(ctx context.Context, path string, hb chan<- struct{}) ([]orphanReport, error) {
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
	sm := security.NewSecurityManager(nil)
	idx, _ := newIndexer(".")
	cache := newASTCache()
	mockExec := &mockHealthExecutor{}
	mockRunner := &mockGoRunner{}
	ana := newAnalysisManager(idx, cache, sm, nil, mockExec, infrapersistence.NewOSFileSystem())
	hea := &healthManager{SP: sm, Ana: ana, Exec: mockExec, Runner: mockRunner}

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
	sm := security.NewSecurityManager(nil)
	idx, _ := newIndexer(".")
	cache := newASTCache()
	mockExec := &mockHealthExecutor{}
	mockRunner := &mockGoRunner{}
	ana := newAnalysisManager(idx, cache, sm, nil, mockExec, infrapersistence.NewOSFileSystem())
	hea := &healthManager{SP: sm, Ana: ana, Exec: mockExec, Runner: mockRunner}

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
			want: []string{"Fix failing tests immediately."},
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
				Ana: &analysisManager{DeadCode: mockAna},
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

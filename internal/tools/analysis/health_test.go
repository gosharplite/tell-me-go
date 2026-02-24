// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"strings"
	"testing"

	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

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
	ana := newAnalysisManager(idx, cache, sm, nil, mockExec, infrapersistence.NewOSFileSystem())
	hea := &healthManager{SP: sm, Ana: ana, Exec: mockExec}

	ctx := context.Background()
	res, err := hea.GetCodeHealth(ctx, nil)
	if err != nil {
		t.Fatalf("GetCodeHealth failed: %v", err)
	}

	if !strings.Contains(res.Text, "Project Health Dashboard") {
		t.Errorf("expected dashboard title, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "| Metric | Status | Details |") {
		t.Errorf("expected table header, got %q", res.Text)
	}
	if strings.Contains(res.Text, "| **Dead Code** |") {
		t.Errorf("did not expect Dead Code row, got %q", res.Text)
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
	ana := newAnalysisManager(idx, cache, sm, nil, mockExec, infrapersistence.NewOSFileSystem())
	hea := &healthManager{SP: sm, Ana: ana, Exec: mockExec}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	res, err := hea.GetCodeHealth(ctx, nil)
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
		want []string
	}{
		{
			name: "excellent health",
			test: "PASS",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			want: []string{"Project health is excellent."},
		},
		{
			name: "failing tests",
			test: "FAIL",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			want: []string{"Fix failing tests immediately."},
		},
		{
			name: "low coverage",
			test: "PASS",
			cov:  "70.0%",
			lint: "CLEAN",
			comp: "GOOD",
			want: []string{"Coverage (70.0%) is below the 80% target."},
		},
		{
			name: "complexity and linting",
			test: "PASS",
			cov:  "85.0%",
			lint: "5 Issues",
			comp: "3 Alerts",
			want: []string{"Refactor high-complexity functions.", "Address linting issues."},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hea.generateRecommendation(tt.test, tt.cov, tt.lint, tt.comp)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("generateRecommendation() = %q, want it to contain %q", got, w)
				}
			}
		})
	}
}

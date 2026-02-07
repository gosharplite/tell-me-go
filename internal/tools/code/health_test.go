// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/code/analysis"
	"github.com/gosharplite/tell-me-go/internal/tools/code/astutil"
	"github.com/gosharplite/tell-me-go/internal/tools/code/index"
)

func TestHealthManager_GetCodeHealth(t *testing.T) {
	t.Setenv("SKIP_HEALTH_EXECUTION", "true")
	sm := security.NewSecurityManager(nil)
	idx, _ := index.NewIndexer(".")
	cache := astutil.NewASTCache()
	ana := analysis.NewAnalysisManager(idx, cache, sm)
	hea := &HealthManager{SP: sm, Ana: ana}

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
}

func TestHealthManager_GetCodeHealth_Cancelled(t *testing.T) {
	t.Setenv("SKIP_HEALTH_EXECUTION", "true")
	sm := security.NewSecurityManager(nil)
	idx, _ := index.NewIndexer(".")
	cache := astutil.NewASTCache()
	ana := analysis.NewAnalysisManager(idx, cache, sm)
	hea := &HealthManager{SP: sm, Ana: ana}

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
	hea := &HealthManager{}

	tests := []struct {
		name string
		test string
		cov  string
		lint string
		comp string
		sec  string
		want []string
	}{
		{
			name: "excellent health",
			test: "PASS",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			sec:  "CLEAN",
			want: []string{"Project health is excellent."},
		},
		{
			name: "failing tests",
			test: "FAIL",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			sec:  "CLEAN",
			want: []string{"Fix failing tests immediately."},
		},
		{
			name: "low coverage",
			test: "PASS",
			cov:  "70.0%",
			lint: "CLEAN",
			comp: "GOOD",
			sec:  "CLEAN",
			want: []string{"Coverage (70.0%) is below the 80% target."},
		},
		{
			name: "security vulnerabilities",
			test: "PASS",
			cov:  "85.0%",
			lint: "CLEAN",
			comp: "GOOD",
			sec:  "VULNS",
			want: []string{"Review and fix security vulnerabilities."},
		},
		{
			name: "complexity and linting",
			test: "PASS",
			cov:  "85.0%",
			lint: "5 Issues",
			comp: "3 Alerts",
			sec:  "CLEAN",
			want: []string{"Refactor high-complexity functions.", "Address linting issues."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hea.generateRecommendation(tt.test, tt.cov, tt.lint, tt.comp, tt.sec)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("generateRecommendation() = %q, want it to contain %q", got, w)
				}
			}
		})
	}
}

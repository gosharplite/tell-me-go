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

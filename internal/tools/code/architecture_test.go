// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestArchitectureManager_VerifyArchitecture(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	arc := &ArchitectureManager{SP: sm}

	ctx := context.Background()
	res, err := arc.VerifyArchitecture(ctx, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}

	// Given current state has violations (internal/agent imports internal/tools/registry)
	if !strings.Contains(res.Text, "Architectural Integrity Report: ❌ FAILED") {
		t.Errorf("expected report to fail, but it passed: %s", res.Text)
	}

	if !strings.Contains(res.Text, "internal/agent") || !strings.Contains(res.Text, "internal/tools/registry") {
		t.Errorf("expected violation for internal/agent -> internal/tools/registry, got: %s", res.Text)
	}
}

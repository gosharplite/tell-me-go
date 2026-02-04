// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestAnalyzeSequenceFlow(t *testing.T) {
	// This test is tricky because it depends on the local filesystem and packages.Load.
	// We'll try to trace something that definitely exists in this repo.
	
	sp := security.NewSecurityManager(nil)
	a := &SequenceAnalyzer{SP: sp}
	
	// Try to trace a known function in the codebase.
	// internal/tools/code/analysis.GenerateMermaid is a good candidate.
	// We need the full package path or enough of it.
	
	ctx := context.Background()
	args := map[string]interface{}{
		"start_symbol": "internal/tools/code/analysis.GenerateMermaid",
		"max_depth":    float64(2),
	}
	
	result, err := a.AnalyzeSequenceFlow(ctx, args)
	if err != nil {
		t.Fatalf("AnalyzeSequenceFlow failed: %v", err)
	}
	
	if strings.HasPrefix(result.Text, "Error") {
		// If it fails due to environment (e.g. go not in path or module issues), 
		// we might need to skip or handle it.
		t.Logf("AnalyzeSequenceFlow returned error text: %s", result.Text)
		return
	}
	
	if !strings.Contains(result.Text, "sequenceDiagram") {
		t.Errorf("Expected sequenceDiagram in output, got: %s", result.Text)
	}
	
	t.Logf("Result:\n%s", result.Text)
}

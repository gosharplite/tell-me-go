// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

// deadCodeAnalyzer is the local seam for the dead code analysis engine,
// allowing tests to inject mocks without touching the analysis package.
type deadCodeAnalyzer interface {
	GatherOrphanReports(ctx context.Context, root string, deep bool, heartbeat chan<- struct{}) ([]analysis.OrphanReport, error)
}

// newAnalyzer is the constructor for the dead code analyzer, overridable in tests.
var newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
	return analysis.NewDeadCodeAnalyzerForCLI(sp)
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx := context.Background()

	sp := security.NewSecurityManager(nil)
	analyzer := newAnalyzer(sp)

	reports, err := analyzer.GatherOrphanReports(ctx, ".", false, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if len(reports) == 0 {
		fmt.Println("No dead code found.")
		return 0
	}

	for _, r := range reports {
		fmt.Printf("[%s] %s.%s (%s) — %s\n", r.Severity, r.Pkg, r.Symbol, r.Type, r.Reason)
	}

	return 0
}

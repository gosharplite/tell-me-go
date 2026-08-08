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
// It exposes the orphan report (existing) and the ADR-056 Decision 1 exit
// query (report-only).
type deadCodeAnalyzer interface {
	GatherOrphanReports(ctx context.Context, root string, deep bool, heartbeat chan<- struct{}) ([]analysis.OrphanReport, error)
	GatherExitCandidates(ctx context.Context, root string, heartbeat chan<- struct{}) ([]analysis.ExitCandidate, error)
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
	} else {
		for _, r := range reports {
			fmt.Printf("[%s] %s.%s (%s) — %s\n", r.Severity, r.Pkg, r.Symbol, r.Type, r.Reason)
		}
	}

	// ADR-056 Decision 1 exit query: per-seam layer-set evaluation for the
	// internal/domain/ports interfaces. Report-only — the exit code stays 0
	// regardless of candidates (the modelith-layers precedent).
	candidates, err := analyzer.GatherExitCandidates(ctx, ".", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Printf("\n— EXIT CANDIDATES (ADR-056 Decision 1, report-only) —\n")
	fmt.Print(analysis.FormatExitCandidates(candidates))

	return 0
}

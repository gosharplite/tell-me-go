// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
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

// exitQueryMode runs only the ADR-056 Decision 1 exit query (report-only).
// The orphan report is skipped entirely. Used by `make verify-exit-query`.
var exitQueryMode = flag.Bool("exit-query", false, "run only the ADR-056 Decision 1 exit query (report-only)")

// exitQueryVerbose forces the full candidate table even when every
// candidate is a documented ADR-056 stay (quiet mode is the default).
var exitQueryVerbose = flag.Bool("exit-query-verbose", false, "with -exit-query, always print the full candidate table")

// newAnalyzer is the constructor for the dead code analyzer, overridable in tests.
var newAnalyzer = func(sp domain_security.PathValidator) deadCodeAnalyzer {
	return analysis.NewDeadCodeAnalyzerForCLI(sp)
}

func main() {
	flag.Parse()
	os.Exit(run())
}

// run dispatches between the two reporting channels: the orphan scan
// (default, `make dead-code`) and the ADR-056 Decision 1 exit query
// (`-exit-query`, `make verify-exit-query`). Both are report-only — the
// exit code stays 0 regardless of findings.
func run() int {
	ctx := context.Background()

	sp := security.NewSecurityManager(nil)
	analyzer := newAnalyzer(sp)

	if *exitQueryMode {
		return runExitQuery(ctx, analyzer)
	}
	return runOrphanReport(ctx, analyzer)
}

// runOrphanReport prints the DEAD/PRIVATE orphan scan rows (or the "No dead
// code found." note). The exit query has its own channel — see
// runExitQuery — so this function never prints exit-candidates content.
func runOrphanReport(ctx context.Context, analyzer deadCodeAnalyzer) int {
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

	return 0
}

// runExitQuery runs the ADR-056 Decision 1 exit query alone. Quiet by
// default: when every candidate is a documented stay (or there are none),
// SummarizeExitCandidates emits one governance line and we skip the table.
// The full candidate table prints only when a NEW candidate exists (never
// hide an actionable row) or when -exit-query-verbose is set. Report-only:
// the exit code stays 0 regardless of candidates (the modelith-layers
// precedent).
func runExitQuery(ctx context.Context, analyzer deadCodeAnalyzer) int {
	candidates, err := analyzer.GatherExitCandidates(ctx, ".", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if s := analysis.SummarizeExitCandidates(candidates); s != "" && !*exitQueryVerbose {
		fmt.Print(s)
		return 0
	}
	fmt.Printf("\n— EXIT CANDIDATES (ADR-056 Decision 1, report-only) —\n")
	fmt.Print(analysis.FormatExitCandidates(candidates))
	return 0
}

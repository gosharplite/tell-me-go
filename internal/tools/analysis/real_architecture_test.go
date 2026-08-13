// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build arch

package analysis

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
)

var strictArch = flag.Bool("strict-arch", true, "fail test on architecture violations")

// transitiveGateReportOnly is the v1 posture for the ADR-056 transitive
// closure gate (issue #1300): default true = non-failing, report-only. The
// issue's implementation detail pins "default non-failing" for gate v1; the
// ADR's "default-strict" is the post-ratification posture. This flag is
// DISTINCT from -strict-arch, which governs only TestVerifyRealArchitecture.
// Flipping to strict post-ratification = -transitive-gate-report-only=false.
var transitiveGateReportOnly = flag.Bool("transitive-gate-report-only", true, "report-only mode for the transitive closure gate")

func TestVerifyRealArchitecture(t *testing.T) {
	m := &architectureManager{
		SP:  &mockSecurityProvider{},
		idx: getRealArchitectureIndexer(t),
	}

	res, err := m.VerifyArchitecture(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("VerifyArchitecture failed: %v", err)
	}

	if strings.Contains(res.Text, "FAILED") {
		if *strictArch {
			t.Errorf("Architecture validation FAILED:\n%s", res.Text)
		} else {
			t.Logf("Architecture validation FAILED:\n%s", res.Text)
		}
	}
}

// TestVerifyTransitiveClosureGate is the ADR-056 Decision 2 gate: it
// measures the module's transitive import closure against the live
// architect-curated whitelist (docs/architect/TRANSITIVE_IMPORT_WHITELIST.md,
// anchored via findModuleRoot — the loadNonFixCatalog precedent) and prints
// the v1 report separating "decision required" rows from "approved constant"
// rows. Default posture is report-only (non-failing); the test fails only
// when -transitive-gate-report-only=false and decision-required rows exist.
func TestVerifyTransitiveClosureGate(t *testing.T) {
	idx := getRealArchitectureIndexer(t)

	pkgs, err := idx.Packages(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to load packages for transitive gate: %v", err)
	}
	modulePath := detectModulePath(pkgs)
	if modulePath == "" {
		t.Fatal("failed to detect module path for transitive gate")
	}

	graph := buildInternalImportGraph(pkgs, modulePath)
	wl, err := loadTransitiveWhitelist()
	if err != nil {
		t.Fatalf("failed to load transitive import whitelist: %v", err)
	}

	classifications := classifyAllConsumers(graph, wl, modulePath)
	infra := classifyInfraLateralEdges(pkgs, wl, modulePath)
	report := formatTransitiveGateReport(classifications, infra, wl)
	fmt.Print(report)

	var decisionRequired int
	for _, c := range classifications {
		if c.Status == statusDecisionRequired {
			decisionRequired++
		}
	}
	for _, e := range infra {
		if e.Status == statusDecisionRequired {
			decisionRequired++
		}
	}
	if decisionRequired > 0 && !*transitiveGateReportOnly {
		t.Errorf("Transitive closure gate FAILED: %d decision-required consumers (run with -transitive-gate-report-only=false to enforce)", decisionRequired)
	}
}

// getRealArchitectureIndexer creates an indexer against the live project
// module root. It is used only by TestVerifyRealArchitecture, which
// explicitly validates the real project's architecture.
func getRealArchitectureIndexer(tb testing.TB) *indexer {
	tb.Helper()
	dir, err := findModuleRoot()
	if err != nil {
		tb.Fatalf("failed to find module root for architecture indexer: %v", err)
	}
	idx, err := newIndexer(dir)
	if err != nil {
		tb.Fatalf("failed to create architecture indexer: %v", err)
	}
	if err := idx.Refresh(context.Background(), nil); err != nil {
		tb.Fatalf("failed to refresh architecture indexer: %v", err)
	}
	return idx
}

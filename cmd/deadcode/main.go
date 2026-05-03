// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx := context.Background()

	sp := security.NewSecurityManager(nil)
	analyzer := analysis.NewDeadCodeAnalyzerForCLI(sp)

	reports, err := analyzer.GatherOrphanReports(ctx, ".", nil)
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

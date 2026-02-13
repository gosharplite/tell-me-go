// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

func main() {
	priority := flag.String("priority", "", "Minimum priority to show (High, Medium, Low)")
	flag.Parse()

	packagePath := "./..."
	if flag.NArg() > 0 {
		packagePath = flag.Arg(0)
	}

	ctx := context.Background()
	executor := &exec.RealExecutor{}

	if *priority == "" {
		report, err := analysis.GetDetailedCoverageReport(ctx, packagePath, executor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(report)
	} else {
		jsonOutput, err := analysis.GetDetailedCoverageJSON(ctx, packagePath, *priority, executor)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(jsonOutput)
	}
}

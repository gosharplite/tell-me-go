// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
)

func main() {
	priority := flag.String("priority", "", "Minimum priority to show (High, Medium, Low)")
	flag.Parse()

	packagePath := "./..."
	if flag.NArg() > 0 {
		packagePath = flag.Arg(0)
	}

	if *priority == "" {
		report, err := analysis.GetDetailedCoverageReport(packagePath, analysis.ShellRunner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(report)
	} else {
		jsonOutput, err := analysis.GetDetailedCoverageJSON(packagePath, *priority, analysis.ShellRunner)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(jsonOutput)
	}
}

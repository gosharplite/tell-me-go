// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/tools/code"
)

func main() {
	priority := flag.String("priority", "", "Minimum priority to show (High, Medium, Low)")
	flag.Parse()

	packagePath := "./..."
	if flag.NArg() > 0 {
		packagePath = flag.Arg(0)
	}

	if *priority == "" {
		report, err := code.GetDetailedCoverageReport(packagePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(report)
	} else {
		jsonOutput, err := code.GetDetailedCoverageJSON(packagePath, *priority)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(jsonOutput)
	}
}

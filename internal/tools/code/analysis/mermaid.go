// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateMermaid transforms a dependency map into a Mermaid.js diagram.
func GenerateMermaid(graph map[string][]string) string {
	var builder strings.Builder
	builder.WriteString("graph TD\n")

	// Define Subgraphs for Layers
	layers := map[string][]string{}
	for pkg := range graph {
		parts := strings.Split(pkg, "/")
		root := parts[0]
		if root == "" {
			root = "root"
		}
		layers[root] = append(layers[root], pkg)
	}

	// Sort layer names for stability
	layerNames := make([]string, 0, len(layers))
	for name := range layers {
		layerNames = append(layerNames, name)
	}
	sort.Strings(layerNames)

	for _, root := range layerNames {
		pkgs := layers[root]
		sort.Strings(pkgs)
		builder.WriteString(fmt.Sprintf("  subgraph %s\n", sanitize(root)))
		for _, p := range pkgs {
			builder.WriteString(fmt.Sprintf("    %s\n", sanitize(p)))
		}
		builder.WriteString("  end\n")
	}

	// Define Relationships
	srcs := make([]string, 0, len(graph))
	for src := range graph {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)

	for _, src := range srcs {
		deps := graph[src]
		sort.Strings(deps)
		for _, dst := range deps {
			builder.WriteString(fmt.Sprintf("  %s --> %s\n", sanitize(src), sanitize(dst)))
		}
	}

	// Styling
	builder.WriteString("\n  classDef transport fill:#f9f,stroke:#333,stroke-width:2px;\n")
	builder.WriteString("  classDef domain fill:#dfd,stroke:#333,stroke-width:2px;\n")
	builder.WriteString("  classDef infrastructure fill:#fdd,stroke:#333,stroke-width:2px;\n")

	for _, pkg := range srcs {
		if strings.Contains(pkg, "api") || strings.Contains(pkg, "transport") {
			builder.WriteString(fmt.Sprintf("  class %s transport;\n", sanitize(pkg)))
		} else if strings.Contains(pkg, "domain") {
			builder.WriteString(fmt.Sprintf("  class %s domain;\n", sanitize(pkg)))
		} else if strings.Contains(pkg, "infrastructure") || strings.Contains(pkg, "internal/tools") {
			builder.WriteString(fmt.Sprintf("  class %s infrastructure;\n", sanitize(pkg)))
		}
	}

	return builder.String()
}

func sanitize(name string) string {
	return strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(name)
}

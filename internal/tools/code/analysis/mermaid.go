// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// StyleRule defines a regex pattern and the CSS class to apply.
type StyleRule struct {
	Pattern string
	Class   string
}

// DefaultStyleRules provides a set of regex patterns for Clean Architecture layers.
var DefaultStyleRules = []StyleRule{
	{Pattern: ".*(api|transport).*", Class: "transport"},
	{Pattern: ".*domain.*", Class: "domain"},
	{Pattern: ".*(infra|tools|security|fsutil).*", Class: "infrastructure"},
}

// GenerateMermaid transforms a dependency map into a Mermaid.js diagram.
func GenerateMermaid(graph map[string][]string) string {
	var builder strings.Builder
	builder.WriteString("graph TD\n")

	cycleEdges := markCycleEdges(graph)
	renderSubgraphs(&builder, graph)
	cycleEdgeIndices := renderRelationships(&builder, graph, cycleEdges)

	pkgs := make([]string, 0, len(graph))
	for pkg := range graph {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	renderStyles(&builder, pkgs, cycleEdgeIndices)

	return builder.String()
}

func markCycleEdges(graph map[string][]string) map[string]bool {
	cycles := findCycles(graph)
	cycleEdges := make(map[string]bool)
	for _, cycle := range cycles {
		for i := 0; i < len(cycle)-1; i++ {
			edgeKey := fmt.Sprintf("%s|%s", cycle[i], cycle[i+1])
			cycleEdges[edgeKey] = true
		}
	}
	return cycleEdges
}

func renderSubgraphs(sb *strings.Builder, graph map[string][]string) {
	layers := map[string][]string{}
	for pkg := range graph {
		parts := strings.Split(pkg, "/")
		root := parts[0]
		if root == "" {
			root = "root"
		}
		layers[root] = append(layers[root], pkg)
	}

	layerNames := make([]string, 0, len(layers))
	for name := range layers {
		layerNames = append(layerNames, name)
	}
	sort.Strings(layerNames)

	for _, root := range layerNames {
		pkgs := layers[root]
		sort.Strings(pkgs)
		sb.WriteString(fmt.Sprintf("  subgraph %s[\"%s\"]\n", sanitize(root), root))
		for _, p := range pkgs {
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", sanitize(p), p))
		}
		sb.WriteString("  end\n")
	}
}

func renderRelationships(sb *strings.Builder, graph map[string][]string, cycleEdges map[string]bool) []string {
	srcs := make([]string, 0, len(graph))
	for src := range graph {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)

	edgeIndex := 0
	var cycleEdgeIndices []string
	for _, src := range srcs {
		deps := graph[src]
		sort.Strings(deps)
		for _, dst := range deps {
			edgeKey := fmt.Sprintf("%s|%s", src, dst)
			if cycleEdges[edgeKey] {
				sb.WriteString(fmt.Sprintf("  %s -->|cycle| %s\n", sanitize(src), sanitize(dst)))
				cycleEdgeIndices = append(cycleEdgeIndices, fmt.Sprint(edgeIndex))
			} else {
				sb.WriteString(fmt.Sprintf("  %s --> %s\n", sanitize(src), sanitize(dst)))
			}
			edgeIndex++
		}
	}
	return cycleEdgeIndices
}

func renderStyles(sb *strings.Builder, pkgs []string, cycleEdgeIndices []string) {
	sb.WriteString("\n  classDef transport fill:#f9f,stroke:#333,stroke-width:2px;\n")
	sb.WriteString("  classDef domain fill:#dfd,stroke:#333,stroke-width:2px;\n")
	sb.WriteString("  classDef infrastructure fill:#fdd,stroke:#333,stroke-width:2px;\n")

	for _, pkg := range pkgs {
		for _, rule := range DefaultStyleRules {
			matched, _ := regexp.MatchString(rule.Pattern, pkg)
			if matched {
				sb.WriteString(fmt.Sprintf("  class %s %s;\n", sanitize(pkg), rule.Class))
				break
			}
		}
	}

	if len(cycleEdgeIndices) > 0 {
		sb.WriteString(fmt.Sprintf("  linkStyle %s stroke:#f00,stroke-width:4px;\n", strings.Join(cycleEdgeIndices, ",")))
	}
}

func findCycles(graph map[string][]string) [][]string {
	var cycles [][]string
	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	var path []string

	var dfs func(string)
	dfs = func(u string) {
		visited[u] = true
		onStack[u] = true
		path = append(path, u)

		deps := graph[u]
		// Sort deps for deterministic cycle detection if multiple exist
		sort.Strings(deps)
		for _, v := range deps {
			if !visited[v] {
				dfs(v)
			} else if onStack[v] {
				cycleStart := -1
				for i, node := range path {
					if node == v {
						cycleStart = i
						break
					}
				}
				if cycleStart != -1 {
					cycle := append([]string{}, path[cycleStart:]...)
					cycle = append(cycle, v)
					cycles = append(cycles, cycle)
				}
			}
		}

		onStack[u] = false
		path = path[:len(path)-1]
	}

	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	for _, n := range nodes {
		if !visited[n] {
			dfs(n)
		}
	}
	return cycles
}

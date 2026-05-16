// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// styleRule defines a regex pattern and the CSS class to apply.
type styleRule struct {
	Pattern *regexp.Regexp
	Class   string
}

// DefaultstyleRules provides a set of regex patterns for Clean Architecture layers.
var defaultstyleRules = []styleRule{
	{Pattern: regexp.MustCompile(`.*(api|transport).*`), Class: "transport"},
	{Pattern: regexp.MustCompile(`.*domain.*`), Class: "domain"},
	{Pattern: regexp.MustCompile(`.*(infra|tools|security|storage).*`), Class: "infrastructure"},
}

// GenerateMermaid transforms a dependency map into a Mermaid.js diagram.
func generateMermaid(graph map[string][]string) string {
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
		_, _ = fmt.Fprintf(sb, "  subgraph %s[\"%s\"]\n", sanitize(root), root)
		for _, p := range pkgs {
			_, _ = fmt.Fprintf(sb, "    %s[\"%s\"]\n", sanitize(p), p)
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
				_, _ = fmt.Fprintf(sb, "  %s -->|cycle| %s\n", sanitize(src), sanitize(dst))
				cycleEdgeIndices = append(cycleEdgeIndices, fmt.Sprint(edgeIndex))
			} else {
				_, _ = fmt.Fprintf(sb, "  %s --> %s\n", sanitize(src), sanitize(dst))
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
		for _, rule := range defaultstyleRules {
			if rule.Pattern.MatchString(pkg) {
				_, _ = fmt.Fprintf(sb, "  class %s %s;\n", sanitize(pkg), rule.Class)
				break
			}
		}
	}

	if len(cycleEdgeIndices) > 0 {
		_, _ = fmt.Fprintf(sb, "  linkStyle %s stroke:#f00,stroke-width:4px;\n", strings.Join(cycleEdgeIndices, ","))
	}
}

type cycleDetector struct {
	graph   map[string][]string
	visited map[string]bool
	onStack map[string]bool
	path    []string
	cycles  [][]string
}

func (d *cycleDetector) dfs(u string) {
	d.visited[u] = true
	d.onStack[u] = true
	d.path = append(d.path, u)

	deps := d.graph[u]
	// Sort deps for deterministic cycle detection if multiple exist
	sort.Strings(deps)
	for _, v := range deps {
		if v == u {
			// Self-loops are trivial; skip cycle marking
			continue
		}
		if !d.visited[v] {
			d.dfs(v)
		} else if d.onStack[v] {
			cycle := d.buildCycle(v)
			if cycle != nil {
				d.cycles = append(d.cycles, cycle)
			}
		}
	}

	d.onStack[u] = false
	d.path = d.path[:len(d.path)-1]
}

func (d *cycleDetector) buildCycle(v string) []string {
	for i, node := range d.path {
		if node == v {
			cycle := make([]string, len(d.path)-i+1)
			copy(cycle, d.path[i:])
			cycle[len(cycle)-1] = v
			return cycle
		}
	}
	return nil
}

func findCycles(graph map[string][]string) [][]string {
	d := &cycleDetector{
		graph:   graph,
		visited: make(map[string]bool),
		onStack: make(map[string]bool),
	}

	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	for _, n := range nodes {
		if !d.visited[n] {
			d.dfs(n)
		}
	}
	return d.cycles
}

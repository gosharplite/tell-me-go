// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"strings"
	"testing"
)

func TestGenerateMermaid(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"cmd/app":         {"internal/api", "internal/domain"},
		"internal/api":    {"internal/domain"},
		"internal/domain": {},
		"internal/infra":  {"internal/domain"},
	}

	result := generateMermaid(graph)

	expectedNodes := []string{
		"cmd_app",
		"internal_api",
		"internal_domain",
		"internal_infra",
	}

	for _, node := range expectedNodes {
		if !strings.Contains(result, node) {
			t.Errorf("Expected result to contain node %s, but it didn't", node)
		}
	}

	expectedEdges := []string{
		"cmd_app --> internal_api",
		"cmd_app --> internal_domain",
		"internal_api --> internal_domain",
		"internal_infra --> internal_domain",
	}

	for _, edge := range expectedEdges {
		if !strings.Contains(result, edge) {
			t.Errorf("Expected result to contain edge %s, but it didn't", edge)
		}
	}

	expectedSubgraphs := []string{
		"subgraph cmd",
		"subgraph internal",
	}

	for _, sg := range expectedSubgraphs {
		if !strings.Contains(result, sg) {
			t.Errorf("Expected result to contain subgraph %s, but it didn't", sg)
		}
	}

	// Check styling
	if !strings.Contains(result, "classDef transport") {
		t.Error("Expected result to contain classDef transport")
	}
	if !strings.Contains(result, "class internal_api transport") {
		t.Error("Expected result to contain class internal_api transport")
	}
}

func TestGenerateMermaidWithCycles(t *testing.T) {
	t.Parallel()
	graph := map[string][]string{
		"pkg/a": {"pkg/b"},
		"pkg/b": {"pkg/c"},
		"pkg/c": {"pkg/a"},
	}

	result := generateMermaid(graph)

	if !strings.Contains(result, "-->|cycle|") {
		t.Error("Expected result to contain cycle label")
	}
	if !strings.Contains(result, "linkStyle") {
		t.Error("Expected result to contain linkStyle for cycle highlighting")
	}
	if !strings.Contains(result, "stroke:#f00") {
		t.Error("Expected result to contain red stroke for cycles")
	}
}

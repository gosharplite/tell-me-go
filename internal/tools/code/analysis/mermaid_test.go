// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"strings"
	"testing"
)

func TestGenerateMermaid(t *testing.T) {
	graph := map[string][]string{
		"cmd/app":           {"internal/api", "internal/domain"},
		"internal/api":       {"internal/domain"},
		"internal/domain":    {},
		"internal/infra":     {"internal/domain"},
	}

	result := GenerateMermaid(graph)

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

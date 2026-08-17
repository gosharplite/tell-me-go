// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// TestToOpenAISchema_EmptyTypeOmitsKey pins the wire-level fix for
// issue #1378: a schema node with an empty Type (produced by
// convertObject for nested unrepresentable nodes — combinators,
// unions, "null", absent type) must marshal to NO "type" key, not to
// `"type":""`. OpenAI rejects `"type":""` with HTTP 400, poisoning the
// entire tools payload. The omitempty tag turns an empty Type into a
// valid JSON Schema "any" node.
func TestToOpenAISchema_EmptyTypeOmitsKey(t *testing.T) {
	tests := []struct {
		name    string
		schema  *tools.Schema
		wantKey bool // whether the wire must contain the "type" key
	}{
		{
			name:    "description only",
			schema:  &tools.Schema{Type: "", Description: "x"},
			wantKey: false,
		},
		{
			name:    "fully empty node",
			schema:  &tools.Schema{},
			wantKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(toOpenAISchema(tt.schema))
			if err != nil {
				t.Fatalf("json.Marshal(toOpenAISchema) failed: %v", err)
			}

			// Robust check: unmarshal and confirm the key is absent.
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
			}
			_, hasType := m["type"]
			if tt.wantKey && !hasType {
				t.Errorf("expected \"type\" key in wire JSON, got %s", b)
			}
			if !tt.wantKey && hasType {
				t.Errorf("expected NO \"type\" key in wire JSON, got %s", b)
			}

			// Exact substring guard: Go's encoder emits no space after
			// the colon — `"type":""`, not `"type": ""`.
			if strings.Contains(string(b), `"type":""`) {
				t.Errorf("wire JSON contains the OpenAI-rejected `\"type\":\"\"`: %s", b)
			}

			if tt.schema.Description != "" {
				if !strings.Contains(string(b), `"description":"`+tt.schema.Description+`"`) {
					t.Errorf("expected description %q in wire JSON, got %s", tt.schema.Description, b)
				}
			}
		})
	}
}

// TestToOpenAISchema_EnumOnlyOmitsType pins that an enum-only node
// (no type) still carries its enum on the wire without a "type" key.
func TestToOpenAISchema_EnumOnlyOmitsType(t *testing.T) {
	tests := []struct {
		name   string
		schema *tools.Schema
		enum   []string
	}{
		{
			name:   "enum only",
			schema: &tools.Schema{Enum: []string{"a", "b"}},
			enum:   []string{"a", "b"},
		},
		{
			name:   "enum with description",
			schema: &tools.Schema{Enum: []string{"x"}, Description: "d"},
			enum:   []string{"x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(toOpenAISchema(tt.schema))
			if err != nil {
				t.Fatalf("json.Marshal(toOpenAISchema) failed: %v", err)
			}

			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
			}
			if _, hasType := m["type"]; hasType {
				t.Errorf("expected NO \"type\" key for enum-only node, got %s", b)
			}

			wantEnum, _ := json.Marshal(tt.enum)
			if !strings.Contains(string(b), `"enum":`+string(wantEnum)) {
				t.Errorf("expected enum %s in wire JSON, got %s", wantEnum, b)
			}
		})
	}
}

// TestToOpenAITools_EmptyTypeNested_OmitsEmptyTypeKey is the end-to-end
// regression test for the issue's exact failure shape: a tools payload
// containing a nested node with an empty Type must NOT contain
// `"type":""` anywhere (previously that single empty key poisoned the
// entire request with HTTP 400), must keep the root object type, must
// preserve the property, and must keep `required` unpruned — nothing is
// dropped, so nothing dangles.
func TestToOpenAITools_EmptyTypeNested_OmitsEmptyTypeKey(t *testing.T) {
	decls := []*tools.ToolDeclaration{{
		Name:        "mcp_github_run_secret_scanning",
		Description: "Scans a repository for secrets",
		Parameters: &tools.Schema{
			Type:        "OBJECT",
			Description: "Input schema",
			Properties: map[string]*tools.Schema{
				"files": {Type: "", Description: "Files to scan"},
				"owner": {Type: "STRING"},
				"repo":  {Type: "STRING"},
			},
			Required: []string{"files", "owner", "repo"},
		},
	}}

	c := NewClient("", "gpt-4", nil)
	res := c.toOpenAITools(decls, false)

	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal(toOpenAITools) failed: %v", err)
	}

	// Hard-400 root cause is gone: no empty type ANYWHERE in the payload.
	if strings.Contains(string(b), `"type":""`) {
		t.Errorf("wire JSON still contains the OpenAI-rejected `\"type\":\"\"`: %s", b)
	}

	// The root keeps its typed "object".
	if !strings.Contains(string(b), `"type":"object"`) {
		t.Errorf("expected root to keep \"type\":\"object\", got %s", b)
	}

	var toolsArr []map[string]any
	if err := json.Unmarshal(b, &toolsArr); err != nil {
		t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
	}
	if len(toolsArr) != 1 {
		t.Fatalf("expected 1 tool, got %d: %s", len(toolsArr), b)
	}

	fn, ok := toolsArr[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"function\" wrapper, got %s", b)
	}
	params, ok := fn["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"parameters\", got %s", b)
	}

	// The nested untyped node keeps its property but loses the type key.
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"properties\", got %s", b)
	}
	files, ok := props["files"].(map[string]any)
	if !ok {
		t.Fatalf("expected \"files\" property, got %s", b)
	}
	if _, hasType := files["type"]; hasType {
		t.Errorf("expected NO \"type\" key on nested \"files\" node, got %s", b)
	}
	if desc, _ := files["description"].(string); desc != "Files to scan" {
		t.Errorf("expected \"files\" description %q, got %q", "Files to scan", desc)
	}

	// required is unpruned: nothing was dropped, so nothing dangles.
	req, ok := params["required"].([]any)
	if !ok {
		t.Fatalf("expected \"required\" array, got %s", b)
	}
	if len(req) != 3 {
		t.Fatalf("expected required [files owner repo] (unpruned), got %v", req)
	}
	for i, want := range []string{"files", "owner", "repo"} {
		if got, _ := req[i].(string); got != want {
			t.Errorf("required[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestToOpenAISchema_TypedNodesKeepType is a regression guard against
// over-omission: typed nodes must keep their (lowercased) type on the
// wire after the omitempty change.
func TestToOpenAISchema_TypedNodesKeepType(t *testing.T) {
	tests := []struct {
		name   string
		schema *tools.Schema
	}{
		{
			name: "object with typed property",
			schema: &tools.Schema{
				Type: "OBJECT",
				Properties: map[string]*tools.Schema{
					"p1": {Type: "STRING"},
				},
			},
		},
		{
			name:   "string leaf",
			schema: &tools.Schema{Type: "STRING"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(toOpenAISchema(tt.schema))
			if err != nil {
				t.Fatalf("json.Marshal(toOpenAISchema) failed: %v", err)
			}

			if tt.schema.Type != "" {
				want := `"type":"` + strings.ToLower(tt.schema.Type) + `"`
				if !strings.Contains(string(b), want) {
					t.Errorf("expected %s in wire JSON, got %s", want, b)
				}
			}

			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
			}
			if props, ok := m["properties"].(map[string]any); ok {
				p1, ok := props["p1"].(map[string]any)
				if !ok {
					t.Fatalf("expected \"p1\" property, got %s", b)
				}
				if got, _ := p1["type"].(string); got != "string" {
					t.Errorf("expected \"p1\".type = \"string\", got %q", got)
				}
			}
		})
	}
}

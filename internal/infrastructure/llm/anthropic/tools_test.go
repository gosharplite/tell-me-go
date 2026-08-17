// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// inputSchemaFor extracts the wire input_schema of the first tool
// produced by toAnthropicTools, marshalled and unmarshalled into plain
// JSON types (the convention established in client_tools_test.go).
func inputSchemaFor(t *testing.T, decls []*tools.ToolDeclaration) map[string]interface{} {
	t.Helper()
	client := &client{}
	ts := client.toAnthropicTools(decls)
	if len(ts) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(ts))
	}
	b, err := json.Marshal(ts[0])
	if err != nil {
		t.Fatalf("json.Marshal(toAnthropicTools) failed: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
	}
	in, ok := m["input_schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected input_schema object in %s", b)
	}
	return in
}

// assertWireEqual marshals both sides and compares the unmarshalled
// plain maps, normalizing slice/map concrete types — the exact
// convention used by TestToAnthropicSchema in client_tools_test.go.
func assertWireEqual(t *testing.T, got interface{}, want map[string]interface{}) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal(got) failed: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(want) failed: %v", err)
	}
	var gotMap, wantMap map[string]interface{}
	_ = json.Unmarshal(gotJSON, &gotMap)
	_ = json.Unmarshal(wantJSON, &wantMap)
	if !reflect.DeepEqual(gotMap, wantMap) {
		t.Errorf("wire mismatch:\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}

// TestToAnthropicTools_EmptyTypeRoot_ForcesObject pins issue #1378 for
// Anthropic: a NON-nil root schema with an empty Type (an untyped ANY
// node produced by the MCP converter for combinators/unions/"null")
// would previously emit `{}` with NO "type" key. Anthropic's API
// mandates "type":"object" on the root input_schema, so the root is
// forced to "object" — but only at the root, never inside nested nodes.
func TestToAnthropicTools_EmptyTypeRoot_ForcesObject(t *testing.T) {
	in := inputSchemaFor(t, []*tools.ToolDeclaration{{
		Name:        "untyped_tool",
		Description: "Tool whose root schema has no type",
		Parameters:  &tools.Schema{Type: ""},
	}})

	assertWireEqual(t, in, map[string]interface{}{
		"type": "object",
	})
}

// TestToAnthropicTools_TypelessRootWithProperties_PreservesContent is
// grill correction #2: forcing "type":"object" on an untyped root MUST
// NOT collapse the schema to {"type":"object","properties":{}} — the
// converted content (here a property) must survive. Collapsing would
// discard expressible properties and diverge from OpenAI/Gemini.
func TestToAnthropicTools_TypelessRootWithProperties_PreservesContent(t *testing.T) {
	in := inputSchemaFor(t, []*tools.ToolDeclaration{{
		Name: "untyped_root_with_props",
		Parameters: &tools.Schema{
			Properties: map[string]*tools.Schema{
				"owner": {Type: "STRING"},
			},
		},
	}})

	assertWireEqual(t, in, map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"owner": map[string]interface{}{"type": "string"},
		},
	})
}

// TestToAnthropicTools_EmptyTypeNested_OmitsTypeKey pins the
// root-vs-nested asymmetry (acceptance criterion for T3): the root
// keeps its typed "object" while the nested empty-Type node omits the
// "type" key entirely (a valid JSON Schema "any" child inside a typed
// container) instead of emitting the Anthropic-rejected `"type":""`.
// required stays unpruned — nothing is dropped, so nothing dangles.
func TestToAnthropicTools_EmptyTypeNested_OmitsTypeKey(t *testing.T) {
	in := inputSchemaFor(t, []*tools.ToolDeclaration{{
		Name: "mcp_tool",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"files": {Type: "", Description: "Files to scan"},
				"owner": {Type: "STRING"},
			},
			Required: []string{"files", "owner"},
		},
	}})

	assertWireEqual(t, in, map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"files": map[string]interface{}{"description": "Files to scan"},
			"owner": map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{"files", "owner"},
	})

	// Targeted assertions for the acceptance criteria: nested "files"
	// carries its description with NO "type" key; required is unpruned.
	props, ok := in["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %v", in)
	}
	files, ok := props["files"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected files property, got %v", props)
	}
	if _, hasType := files["type"]; hasType {
		t.Errorf("expected NO \"type\" key on nested \"files\" node, got %v", files)
	}
	if desc, _ := files["description"].(string); desc != "Files to scan" {
		t.Errorf("expected \"files\" description %q, got %q", "Files to scan", desc)
	}
	req, ok := in["required"].([]interface{})
	if !ok || len(req) != 2 || req[0] != "files" || req[1] != "owner" {
		t.Errorf("expected required [files owner] unpruned, got %v", in["required"])
	}
}

// TestToAnthropicTools_EnumOnlyNested_OmitsTypeKey pins that an
// enum-only nested node (no type) keeps its enum on the wire while
// omitting the "type" key.
func TestToAnthropicTools_EnumOnlyNested_OmitsTypeKey(t *testing.T) {
	in := inputSchemaFor(t, []*tools.ToolDeclaration{{
		Name: "enum_tool",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"level": {Enum: []string{"a", "b"}},
			},
		},
	}})

	assertWireEqual(t, in, map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"level": map[string]interface{}{"enum": []interface{}{"a", "b"}},
		},
	})
}

// TestToAnthropicSchema_EmptyTypeOmitsKey directly pins the production
// change in toAnthropicSchema: a node with an empty Type must not emit
// `"type":""` (Anthropic rejects it) — the key is omitted entirely,
// while description and enum are preserved.
func TestToAnthropicSchema_EmptyTypeOmitsKey(t *testing.T) {
	tests := []struct {
		name   string
		schema *tools.Schema
		want   map[string]interface{}
	}{
		{
			name:   "fully empty node",
			schema: &tools.Schema{},
			want:   map[string]interface{}{},
		},
		{
			name:   "description only",
			schema: &tools.Schema{Description: "x"},
			want:   map[string]interface{}{"description": "x"},
		},
		{
			name:   "enum only",
			schema: &tools.Schema{Enum: []string{"a", "b"}},
			want:   map[string]interface{}{"enum": []interface{}{"a", "b"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toAnthropicSchema(tt.schema)
			assertWireEqual(t, got, tt.want)
		})
	}
}

// TestToAnthropicSchema_TypedNodesUnchanged is a regression guard:
// every node with a non-empty Type must keep its lowercased "type" key
// on the wire exactly as before the conditional-key change. These
// mirror the pre-existing TestToAnthropicSchema cases in
// client_tools_test.go, which must keep passing untouched.
func TestToAnthropicSchema_TypedNodesUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		schema *tools.Schema
		want   map[string]interface{}
	}{
		{
			name:   "string with description",
			schema: &tools.Schema{Type: "STRING", Description: "A string"},
			want: map[string]interface{}{
				"type":        "string",
				"description": "A string",
			},
		},
		{
			name: "object with properties and required",
			schema: &tools.Schema{
				Type: "OBJECT",
				Properties: map[string]*tools.Schema{
					"name": {Type: "STRING"},
				},
				Required: []string{"name"},
			},
			want: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toAnthropicSchema(tt.schema)
			assertWireEqual(t, got, tt.want)
		})
	}
}

// TestToAnthropicTools_NilParameters_KeepsShim is a regression guard
// for the parameterless-tool fallback: a nil Parameters schema must
// still produce the exact Anthropic-required
// {"type":"object","properties":{}} — the same case pinned by
// TestToAnthropicTools in client_tools_test.go, which must keep
// passing untouched.
func TestToAnthropicTools_NilParameters_KeepsShim(t *testing.T) {
	in := inputSchemaFor(t, []*tools.ToolDeclaration{{
		Name:        "parameterless_tool",
		Description: "A tool with no parameters",
		Parameters:  nil,
	}})

	assertWireEqual(t, in, map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	})
}

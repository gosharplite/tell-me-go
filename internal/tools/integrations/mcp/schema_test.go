// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestConvertSchema_NilOrEmpty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "nil raw", raw: nil},
		{name: "empty raw", raw: json.RawMessage("")},
		{name: "empty object", raw: json.RawMessage(`{}`)},
		{name: "json null", raw: json.RawMessage(`null`)},
		{name: "whitespace only", raw: json.RawMessage(`   `)},
		{name: "invalid json", raw: json.RawMessage(`{`)},
		{name: "non-object array", raw: json.RawMessage(`[]`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ConvertSchema(tc.raw, "test_tool"); got != nil {
				t.Errorf("ConvertSchema() = %+v, want nil", got)
			}
		})
	}
}

func TestConvertSchema_CombinatorDegradation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "oneOf", raw: json.RawMessage(`{"oneOf":[{"type":"string"},{"type":"integer"}]}`)},
		{name: "anyOf", raw: json.RawMessage(`{"anyOf":[{"type":"string"}]}`)},
		{name: "allOf", raw: json.RawMessage(`{"allOf":[{"type":"object"}]}`)},
		{name: "ref", raw: json.RawMessage(`{"$ref":"#/definitions/foo"}`)},
		{name: "union type", raw: json.RawMessage(`{"type":["string","null"]}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ConvertSchema(tc.raw, "test_tool"); got != nil {
				t.Errorf("ConvertSchema() = %+v, want nil for combinator schema", got)
			}
		})
	}
}

func TestConvertSchema_TypeNormalization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawType string
		want    string
	}{
		{rawType: `"object"`, want: "OBJECT"},
		{rawType: `"string"`, want: "STRING"},
		{rawType: `"integer"`, want: "INTEGER"},
		{rawType: `"boolean"`, want: "BOOLEAN"},
		{rawType: `"array"`, want: "ARRAY"},
		{rawType: `"number"`, want: "NUMBER"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			raw := json.RawMessage(`{"type":` + tc.rawType + `}`)
			got := ConvertSchema(raw, "test_tool")
			if got == nil {
				t.Fatal("ConvertSchema() returned nil")
			}
			if got.Type != tc.want {
				t.Errorf("Type = %q, want %q", got.Type, tc.want)
			}
		})
	}
}

func TestConvertSchema_FullObject(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{
		"type": "object",
		"description": "root object",
		"properties": {
			"name": {"type": "string", "description": "a name", "enum": ["a", "b"]},
			"count": {"type": "integer", "minimum": 0, "maximum": 10},
			"tags": {"type": "array", "items": {"type": "string"}}
		},
		"required": ["name"],
		"additionalProperties": false,
		"pattern": "ignored",
		"default": {"name": "x"}
	}`)

	got := ConvertSchema(raw, "test_tool")
	if got == nil {
		t.Fatal("ConvertSchema() returned nil")
	}

	assertFullObjectRoot(t, got)
	assertFullObjectNameProp(t, got.Properties["name"])
	assertFullObjectCountProp(t, got.Properties["count"])
	assertFullObjectTagsProp(t, got.Properties["tags"])
	assertFullObjectRequired(t, got.Required)
}

// assertFullObjectRoot pins the root-level schema of the full-object fixture:
// OBJECT type, description passthrough, and three converted properties.
func assertFullObjectRoot(t *testing.T, got *tools.Schema) {
	t.Helper()
	if got.Type != "OBJECT" {
		t.Errorf("Type = %q, want OBJECT", got.Type)
	}
	if got.Description != "root object" {
		t.Errorf("Description = %q, want %q", got.Description, "root object")
	}
	if len(got.Properties) != 3 {
		t.Fatalf("len(Properties) = %d, want 3", len(got.Properties))
	}
}

// assertFullObjectNameProp pins the "name" string property: STRING type,
// description passthrough, and enum slice preservation.
func assertFullObjectNameProp(t *testing.T, nameProp *tools.Schema) {
	t.Helper()
	if nameProp == nil || nameProp.Type != "STRING" {
		t.Errorf("properties[name] = %+v, want STRING schema", nameProp)
		return
	}
	if nameProp.Description != "a name" {
		t.Errorf("properties[name].Description = %q, want %q", nameProp.Description, "a name")
	}
	if len(nameProp.Enum) != 2 || nameProp.Enum[0] != "a" || nameProp.Enum[1] != "b" {
		t.Errorf("properties[name].Enum = %v, want [a b]", nameProp.Enum)
	}
}

// assertFullObjectCountProp pins the "count" integer property type.
func assertFullObjectCountProp(t *testing.T, countProp *tools.Schema) {
	t.Helper()
	if countProp == nil || countProp.Type != "INTEGER" {
		t.Errorf("properties[count] = %+v, want INTEGER schema", countProp)
	}
}

// assertFullObjectTagsProp pins the "tags" array property: ARRAY type with a
// STRING items schema.
func assertFullObjectTagsProp(t *testing.T, tagsProp *tools.Schema) {
	t.Helper()
	if tagsProp == nil || tagsProp.Type != "ARRAY" {
		t.Fatalf("properties[tags] = %+v, want ARRAY schema", tagsProp)
	}
	if tagsProp.Items == nil || tagsProp.Items.Type != "STRING" {
		t.Errorf("properties[tags].Items = %+v, want STRING schema", tagsProp.Items)
	}
}

// assertFullObjectRequired pins the required-fields slice.
func assertFullObjectRequired(t *testing.T, required []string) {
	t.Helper()
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("Required = %v, want [name]", required)
	}
}

func TestConvertSchema_RequiredAndEnumStringSlices(t *testing.T) {
	t.Parallel()

	// required and enum may arrive either as []interface{} (from JSON) or as
	// pre-decoded []string. The former is the production path; the latter is
	// exercised through direct map construction.
	raw := json.RawMessage(`{"type":"object","required":["a","b"],"enum":["x","y"]}`)
	got := ConvertSchema(raw, "test_tool")
	if got == nil {
		t.Fatal("ConvertSchema() returned nil")
	}
	if len(got.Required) != 2 || got.Required[0] != "a" || got.Required[1] != "b" {
		t.Errorf("Required = %v, want [a b]", got.Required)
	}
	if len(got.Enum) != 2 || got.Enum[0] != "x" || got.Enum[1] != "y" {
		t.Errorf("Enum = %v, want [x y]", got.Enum)
	}
}

func TestConvertSchema_DropsNonStringEnumEntries(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"string","enum":["x",1,true]}`)
	got := ConvertSchema(raw, "test_tool")
	if got == nil {
		t.Fatal("ConvertSchema() returned nil")
	}
	if len(got.Enum) != 1 || got.Enum[0] != "x" {
		t.Errorf("Enum = %v, want [x]", got.Enum)
	}
}

// TestConvertSchema_UnsupportedType_BecomesUntypedAny pins the
// converter-level contract for an unsupported root type: the node is NOT
// dropped — it becomes the canonical untyped ANY (Type == ""). The provider
// wire adapters (OpenAI/Anthropic/Gemini) omit the type key, producing a
// valid JSON Schema "any" node; wire validity is pinned by the adapter tests
// in the internal/infrastructure/llm/{openai,anthropic,gemini} packages.
func TestConvertSchema_UnsupportedType_BecomesUntypedAny(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"null"}`)
	got := ConvertSchema(raw, "test_tool")
	if got == nil {
		t.Fatal("ConvertSchema() returned nil")
	}
	if got.Type != "" {
		t.Errorf("Type = %q, want empty", got.Type)
	}
}

// TestConvertSchema_NestedUnrepresentable_BecomesUntypedAny pins the
// converter-level contract for unrepresentable nodes nested inside
// properties/items: they are NOT dropped and the root OBJECT survives.
// convertObject keeps such nodes as untyped ANY (Type == "" with
// Description/Enum preserved); only ROOT combinators degrade to nil (see
// TestConvertSchema_CombinatorDegradation). The run_secret_scanning fixture
// reproduces the issue's real-world shape: a nested anyOf under
// properties.files, a typed root, and required fields that must stay unpruned
// because nothing is dropped.
func TestConvertSchema_NestedUnrepresentable_BecomesUntypedAny(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		raw            json.RawMessage
		propKey        string
		wantPropDesc   string
		wantPropEnum   []string
		wantExtraProps map[string]string // property name -> expected Type ("" = untyped ANY)
		wantRequired   []string
	}{
		{
			name: "run secret scanning fixture",
			raw: json.RawMessage(`{
				"type": "object",
				"description": "Scans a repository for secrets",
				"properties": {
					"files": {
						"anyOf": [
							{"type": "string"},
							{"type": "array", "items": {"type": "string"}}
						],
						"description": "Files to scan"
					},
					"owner": {"type": "string"},
					"repo": {"type": "string"}
				},
				"required": ["files", "owner", "repo"]
			}`),
			propKey:        "files",
			wantPropDesc:   "Files to scan",
			wantExtraProps: map[string]string{"owner": "STRING", "repo": "STRING"},
			wantRequired:   []string{"files", "owner", "repo"},
		},
		{
			name:    "nested anyOf",
			raw:     json.RawMessage(`{"type":"object","properties":{"p":{"anyOf":[{"type":"string"},{"type":"integer"}]}}}`),
			propKey: "p",
		},
		{
			name:    "nested union type",
			raw:     json.RawMessage(`{"type":"object","properties":{"p":{"type":["string","null"]}}}`),
			propKey: "p",
		},
		{
			name:    "nested null type",
			raw:     json.RawMessage(`{"type":"object","properties":{"p":{"type":"null"}}}`),
			propKey: "p",
		},
		{
			name:         "nested absent type",
			raw:          json.RawMessage(`{"type":"object","properties":{"p":{"description":"just a description"}}}`),
			propKey:      "p",
			wantPropDesc: "just a description",
		},
		{
			name:         "enum only node",
			raw:          json.RawMessage(`{"type":"object","properties":{"p":{"enum":["a","b"]}}}`),
			propKey:      "p",
			wantPropEnum: []string{"a", "b"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ConvertSchema(tc.raw, "test_tool")
			if got == nil {
				t.Fatal("ConvertSchema() returned nil")
			}
			if got.Type != "OBJECT" {
				t.Errorf("root Type = %q, want OBJECT", got.Type)
			}

			// The unrepresentable node survives as an untyped ANY node with
			// its Description/Enum preserved.
			prop := got.Properties[tc.propKey]
			if prop == nil {
				t.Fatalf("properties[%q] = nil, want untyped ANY node", tc.propKey)
			}
			if prop.Type != "" {
				t.Errorf("properties[%q].Type = %q, want empty (untyped ANY)", tc.propKey, prop.Type)
			}
			if prop.Description != tc.wantPropDesc {
				t.Errorf("properties[%q].Description = %q, want %q", tc.propKey, prop.Description, tc.wantPropDesc)
			}
			if !stringSliceEqual(prop.Enum, tc.wantPropEnum) {
				t.Errorf("properties[%q].Enum = %v, want %v", tc.propKey, prop.Enum, tc.wantPropEnum)
			}

			// Typed siblings keep their canonical types.
			for name, wantType := range tc.wantExtraProps {
				extra := got.Properties[name]
				if extra == nil {
					t.Errorf("properties[%q] = nil, want schema with Type %q", name, wantType)
					continue
				}
				if extra.Type != wantType {
					t.Errorf("properties[%q].Type = %q, want %q", name, extra.Type, wantType)
				}
			}

			// required survives unpruned: nothing is dropped, so it never dangles.
			if !stringSliceEqual(got.Required, tc.wantRequired) {
				t.Errorf("Required = %v, want %v", got.Required, tc.wantRequired)
			}
		})
	}
}

// stringSliceEqual reports whether two string slices are element-wise equal.
// nil and empty slices compare equal.
func stringSliceEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

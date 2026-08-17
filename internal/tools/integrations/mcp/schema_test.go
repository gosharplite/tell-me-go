// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"testing"
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

	if got.Type != "OBJECT" {
		t.Errorf("Type = %q, want OBJECT", got.Type)
	}
	if got.Description != "root object" {
		t.Errorf("Description = %q, want %q", got.Description, "root object")
	}

	if len(got.Properties) != 3 {
		t.Fatalf("len(Properties) = %d, want 3", len(got.Properties))
	}

	nameProp := got.Properties["name"]
	if nameProp == nil || nameProp.Type != "STRING" {
		t.Errorf("properties[name] = %+v, want STRING schema", nameProp)
	}
	if nameProp != nil {
		if nameProp.Description != "a name" {
			t.Errorf("properties[name].Description = %q, want %q", nameProp.Description, "a name")
		}
		if len(nameProp.Enum) != 2 || nameProp.Enum[0] != "a" || nameProp.Enum[1] != "b" {
			t.Errorf("properties[name].Enum = %v, want [a b]", nameProp.Enum)
		}
	}

	countProp := got.Properties["count"]
	if countProp == nil || countProp.Type != "INTEGER" {
		t.Errorf("properties[count] = %+v, want INTEGER schema", countProp)
	}

	tagsProp := got.Properties["tags"]
	if tagsProp == nil || tagsProp.Type != "ARRAY" {
		t.Fatalf("properties[tags] = %+v, want ARRAY schema", tagsProp)
	}
	if tagsProp.Items == nil || tagsProp.Items.Type != "STRING" {
		t.Errorf("properties[tags].Items = %+v, want STRING schema", tagsProp.Items)
	}

	if len(got.Required) != 1 || got.Required[0] != "name" {
		t.Errorf("Required = %v, want [name]", got.Required)
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

func TestConvertSchema_UnsupportedTypeDropped(t *testing.T) {
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

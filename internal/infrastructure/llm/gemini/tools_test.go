// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"encoding/json"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"google.golang.org/genai"
)

// TestToSDKSchema_EmptyType_OmitsTypeKey pins issue #1378 for Gemini:
// toSDKSchema maps an empty domain Type to genai.Type("") — the empty
// string. genai.Type is a string type and genai.Schema.Type carries
// `json:"type,omitempty"` (both verified via go doc), so the SDK's own
// omitempty tag drops the key entirely: Gemini is degraded (untyped
// any), never hard-failing. No production change is required or wanted.
func TestToSDKSchema_EmptyType_OmitsTypeKey(t *testing.T) {
	tests := []struct {
		name       string
		schema     *tools.Schema
		wantGoType string   // expected Go value of genai.Schema.Type
		wantDesc   string   // expected description on the wire; "" = absent
		wantEnum   []string // expected enum on the wire; nil = absent
	}{
		{
			name:       "description only",
			schema:     &tools.Schema{Type: "", Description: "x"},
			wantGoType: "",
			wantDesc:   "x",
		},
		{
			name:     "enum only",
			schema:   &tools.Schema{Enum: []string{"a", "b"}},
			wantEnum: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sdkSchema := toSDKSchema(tt.schema)
			if sdkSchema == nil {
				t.Fatal("expected non-nil schema")
			}

			// Go-value pin: the empty domain Type maps to genai.Type("")
			// (the empty string), NOT genai.TypeUnspecified.
			if got := string(sdkSchema.Type); got != tt.wantGoType {
				t.Errorf("expected Type %q, got %q", tt.wantGoType, got)
			}

			b, err := json.Marshal(sdkSchema)
			if err != nil {
				t.Fatalf("json.Marshal(toSDKSchema) failed: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
			}

			// Wire pin: the SDK's own omitempty tag must drop the "type" key.
			if _, hasType := m["type"]; hasType {
				t.Errorf("expected NO \"type\" key in wire JSON, got %s", b)
			}

			if tt.wantDesc != "" {
				if desc, _ := m["description"].(string); desc != tt.wantDesc {
					t.Errorf("expected description %q in wire JSON, got %q", tt.wantDesc, desc)
				}
			}

			if tt.wantEnum != nil {
				enum, ok := m["enum"].([]any)
				if !ok || len(enum) != len(tt.wantEnum) {
					t.Fatalf("expected enum %v in wire JSON, got %v", tt.wantEnum, m["enum"])
				}
				for i, want := range tt.wantEnum {
					if got, _ := enum[i].(string); got != want {
						t.Errorf("enum[%d] = %q, want %q", i, got, want)
					}
				}
			}
		})
	}

	// Grill correction #3: TYPE_UNSPECIFIED must never be set explicitly.
	// genai.Type("") is the empty string, which the SDK's omitempty tag
	// omits. Setting genai.TypeUnspecified instead would serialize as a
	// NON-empty "type":"TYPE_UNSPECIFIED" on the wire — a behavior change
	// this issue must not introduce (Gemini is degraded, never hard-failing).
	t.Run("never_emits_type_unspecified", func(t *testing.T) {
		sdkSchema := toSDKSchema(&tools.Schema{Type: ""})
		if sdkSchema == nil {
			t.Fatal("expected non-nil schema")
		}
		if got := string(sdkSchema.Type); got == string(genai.TypeUnspecified) {
			t.Errorf("TYPE_UNSPECIFIED must never be set explicitly, got %q", got)
		}

		b, err := json.Marshal(sdkSchema)
		if err != nil {
			t.Fatalf("json.Marshal(toSDKSchema) failed: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("json.Unmarshal(%s) failed: %v", b, err)
		}
		if got, _ := m["type"].(string); got == string(genai.TypeUnspecified) {
			t.Errorf("wire JSON must never contain \"type\":\"TYPE_UNSPECIFIED\", got %s", b)
		}
	})
}

// TestToSDKSchema_TypedNodesUnchanged is a Go-value regression guard:
// nodes with a non-empty Type keep their type and all other fields
// exactly as before (mirrors the pre-existing TestToSDKSchema in
// gemini_test.go, which must keep passing untouched). Wire serialization
// of typed nodes is pre-existing behavior and out of scope for this
// issue — only the empty-Type contract is pinned here.
func TestToSDKSchema_TypedNodesUnchanged(t *testing.T) {
	s := &tools.Schema{
		Type:        "OBJECT",
		Description: "test",
		Required:    []string{"prop1"},
		Properties: map[string]*tools.Schema{
			"prop1": {Type: "STRING", Description: "property 1"},
			"prop2": {Type: "ARRAY", Items: &tools.Schema{Type: "INTEGER"}},
		},
	}

	sdkSchema := toSDKSchema(s)

	if got := string(sdkSchema.Type); got != "OBJECT" {
		t.Errorf("expected type %q, got %q", "OBJECT", got)
	}
	if got := sdkSchema.Description; got != "test" {
		t.Errorf("expected description %q, got %q", "test", got)
	}
	if len(sdkSchema.Required) != 1 || sdkSchema.Required[0] != "prop1" {
		t.Errorf("expected required %v, got %v", []string{"prop1"}, sdkSchema.Required)
	}
	if got := string(sdkSchema.Properties["prop1"].Type); got != "STRING" {
		t.Errorf("expected prop1 type %q, got %q", "STRING", got)
	}
	if got := sdkSchema.Properties["prop1"].Description; got != "property 1" {
		t.Errorf("expected prop1 description %q, got %q", "property 1", got)
	}
	if got := string(sdkSchema.Properties["prop2"].Type); got != "ARRAY" {
		t.Errorf("expected prop2 type %q, got %q", "ARRAY", got)
	}
	if got := string(sdkSchema.Properties["prop2"].Items.Type); got != "INTEGER" {
		t.Errorf("expected prop2 items type %q, got %q", "INTEGER", got)
	}
}

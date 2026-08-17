// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"encoding/json"
	"log/slog"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ConvertSchema converts a raw MCP tool input schema (JSON Schema) into the
// canonical tools.Schema representation used by the LLM tool-calling layer.
//
// The conversion is intentionally lossy and conservative:
//   - An empty or absent schema returns nil (the tool accepts freeform args).
//   - A schema whose ROOT node uses combinators (oneOf/anyOf/allOf/$ref) or a
//     root union type ("type": ["string", "null"]) degrades to nil
//     (Parameters=nil freeform args) per ADR-067 §6: the canonical schema
//     cannot faithfully express them, and an incomplete ROOT schema would fail
//     validation at call time.
//   - Nested unrepresentable nodes are NOT dropped: combinators, union types,
//     a "null" type, or an absent type inside properties/items are kept by
//     convertObject as untyped ANY nodes (Type == "" with Description/Enum
//     preserved). The empty Type string is the canonical "ANY" representation;
//     provider wire adapters omit the type key (OpenAI/Anthropic/Gemini),
//     producing a valid JSON Schema "any" node.
//   - Unsupported JSON Schema keywords are dropped silently.
//
// toolName is used only for structured logging when a root schema is degraded.
func ConvertSchema(raw json.RawMessage, toolName string) *tools.Schema {
	if len(raw) == 0 {
		return nil
	}

	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	// Empty JSON object ({}), JSON null, or a non-object payload all map to
	// "no schema": the tool accepts freeform arguments.
	if len(m) == 0 {
		return nil
	}

	if hasCombinators(m) {
		slog.Warn("mcp_tool_schema_combinator_degraded", "tool", toolName, "reason", "schema combinators not supported")
		return nil
	}

	return convertObject(m)
}

// hasCombinators reports whether a schema map uses any construct the
// canonical tools.Schema cannot express. When true, ConvertSchema degrades
// the schema to nil so the LLM tool call is not rejected by schema
// validation against an incomplete schema.
func hasCombinators(m map[string]interface{}) bool {
	for _, key := range []string{"oneOf", "anyOf", "allOf", "$ref"} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	// A union type ("type": ["string", "null"]) unmarshals to []interface{}.
	if _, ok := m["type"].([]interface{}); ok {
		return true
	}
	return false
}

// convertObject recursively converts a single JSON Schema object node into a
// tools.Schema. Unknown/unsupported keywords are dropped.
func convertObject(m map[string]interface{}) *tools.Schema {
	s := &tools.Schema{}

	if t, ok := m["type"]; ok {
		s.Type = normalizeType(t)
	}
	if d, ok := m["description"].(string); ok {
		s.Description = d
	}
	if props, ok := m["properties"].(map[string]interface{}); ok {
		converted := make(map[string]*tools.Schema, len(props))
		for name, v := range props {
			if pm, ok := v.(map[string]interface{}); ok {
				converted[name] = convertObject(pm)
			}
		}
		if len(converted) > 0 {
			s.Properties = converted
		}
	}
	if items, ok := m["items"].(map[string]interface{}); ok {
		s.Items = convertObject(items)
	}
	if req, ok := m["required"]; ok {
		s.Required = toStringSlice(req)
	}
	if enum, ok := m["enum"]; ok {
		s.Enum = toStringSlice(enum)
	}

	return s
}

// normalizeType maps a JSON Schema primitive type name to the canonical
// UPPERCASE tools.Schema type. Unknown types return the empty string.
func normalizeType(t interface{}) string {
	name, ok := t.(string)
	if !ok {
		return ""
	}
	switch name {
	case "object":
		return "OBJECT"
	case "string":
		return "STRING"
	case "integer":
		return "INTEGER"
	case "boolean":
		return "BOOLEAN"
	case "array":
		return "ARRAY"
	case "number":
		return "NUMBER"
	default:
		return ""
	}
}

// toStringSlice converts a JSON array ([]interface{} after unmarshal) or a
// []string into a []string, dropping non-string entries.
func toStringSlice(v interface{}) []string {
	switch arr := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), arr...)
	default:
		return nil
	}
}

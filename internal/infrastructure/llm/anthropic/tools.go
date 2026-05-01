// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func (c *client) toAnthropicTools(decls []*tools.ToolDeclaration) []tool {
	if len(decls) == 0 {
		return nil
	}
	res := make([]tool, 0, len(decls))
	for _, d := range decls {
		schema := toAnthropicSchema(d.Parameters)
		if schema == nil {
			// Anthropic requires a valid object schema even for parameterless tools
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		res = append(res, tool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schema,
		})
	}
	return res
}

func toAnthropicSchema(s *tools.Schema) interface{} {
	if s == nil {
		return nil
	}
	res := map[string]interface{}{
		"type": strings.ToLower(s.Type),
	}
	if s.Description != "" {
		res["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		res["enum"] = s.Enum
	}

	// Only add properties if there are entries
	if len(s.Properties) > 0 {
		props := make(map[string]interface{})
		for k, v := range s.Properties {
			props[k] = toAnthropicSchema(v)
		}
		res["properties"] = props
	}

	// Only add required if there are entries
	if len(s.Required) > 0 {
		res["required"] = s.Required
	}

	// Only add items for arrays
	if strings.ToLower(s.Type) == "array" && s.Items != nil {
		res["items"] = toAnthropicSchema(s.Items)
	}

	return res
}

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
	setSchemaDescription(res, s.Description)
	setSchemaEnum(res, s.Enum)
	setSchemaProperties(res, s.Properties)
	setSchemaItems(res, s)
	return res
}

func setSchemaDescription(res map[string]interface{}, desc string) {
	if desc != "" {
		res["description"] = desc
	}
}

func setSchemaEnum(res map[string]interface{}, enum []string) {
	if len(enum) > 0 {
		res["enum"] = enum
	}
}

func setSchemaProperties(res map[string]interface{}, props map[string]*tools.Schema) {
	if len(props) > 0 {
		m := make(map[string]interface{})
		for k, v := range props {
			m[k] = toAnthropicSchema(v)
		}
		res["properties"] = m
	}
}

func setSchemaItems(res map[string]interface{}, s *tools.Schema) {
	if len(s.Required) > 0 {
		res["required"] = s.Required
	}
	if strings.ToLower(s.Type) == "array" && s.Items != nil {
		res["items"] = toAnthropicSchema(s.Items)
	}
}

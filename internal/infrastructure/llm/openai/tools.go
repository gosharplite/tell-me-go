package openai

import (
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func (c *client) toOpenAITools(decls []*tools.ToolDeclaration, flattened bool) []tool {
	if len(decls) == 0 {
		return nil
	}
	var res []tool
	for _, d := range decls {
		t := tool{
			Type: "function",
		}
		if flattened {
			t.Name = d.Name
			t.Description = d.Description
			t.Parameters = toOpenAISchema(d.Parameters)
		} else {
			t.Function = &functionDeclaration{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  toOpenAISchema(d.Parameters),
			}
		}
		res = append(res, t)
	}
	return res
}

func toOpenAISchema(s *tools.Schema) *schema {
	if s == nil {
		return nil
	}
	res := &schema{
		Type:        strings.ToLower(s.Type),
		Description: s.Description,
		Required:    s.Required,
		Enum:        s.Enum,
		Items:       toOpenAISchema(s.Items),
	}
	if s.Properties != nil {
		res.Properties = make(map[string]*schema)
		for k, v := range s.Properties {
			res.Properties[k] = toOpenAISchema(v)
		}
	}
	return res
}

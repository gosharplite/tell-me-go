// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// toolDeclarationGenerator injects tool schemas from the registry.
type toolDeclarationGenerator struct {
	Registry ToolRegistry
}

func (t *toolDeclarationGenerator) Transform(ctx context.Context, req *services.ContextRequest) error {
	if t.Registry == nil {
		return nil
	}

	// Safety: check for typed nil (e.g., *registry.Registry(nil))
	v := reflect.ValueOf(t.Registry)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}

	decls := t.Registry.GetDeclarations()
	if len(decls) == 0 || len(req.History) == 0 {
		return nil
	}

	// 1. Serialize tools to a readable format
	toolJSON, err := json.MarshalIndent(decls, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize tools: %w", err)
	}

	injection := fmt.Sprintf("\n\n# AVAILABLE_TOOLS\nYou may use the following tools via function calls:\n%s", string(toolJSON))

	// 2. Clone the first message to avoid "History Pollution" in long-term memory.
	// We replace the pointer in the current request's history slice.
	firstMsg := req.History[0]
	cloned := firstMsg.Clone()

	// 3. Append the tool schemas to TransientParts
	cloned.TransientParts = append(cloned.TransientParts, &llm.Part{Text: injection})

	// 4. Update the request history slice
	req.History[0] = cloned
	return nil
}

func (t *toolDeclarationGenerator) Priority() int { return 75 }

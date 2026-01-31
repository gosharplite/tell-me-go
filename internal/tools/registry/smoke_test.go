// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package registry_test

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestRegistrySmoke(t *testing.T) {
	r := registry.New()

	r.Register(&types.ToolDeclaration{
		Name:        "test_tool",
		Description: "A test tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{Text: "OK"}, nil
	})

	res, err := r.Execute(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("failed to execute tool: %v", err)
	}
	if res.Text != "OK" {
		t.Errorf("got %s, want OK", res.Text)
	}
}

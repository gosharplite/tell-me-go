// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package registry_test

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

func TestRegistrySmoke(t *testing.T) {
	t.Parallel()
	r := registry.New()

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "test_tool",
		Description: "A test tool",
	}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "OK"}, nil
	}); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	res, err := r.Execute(context.Background(), "test_tool", nil, nil)
	if err != nil {
		t.Fatalf("failed to execute tool: %v", err)
	}
	if res.Text != "OK" {
		t.Errorf("got %s, want OK", res.Text)
	}
}

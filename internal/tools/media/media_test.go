// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package media

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type mockAgentGateway struct {
	generateImageFunc func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
	readImageFunc     func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error)
}

func (m *mockAgentGateway) GenerateImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if m.generateImageFunc != nil {
		return m.generateImageFunc(ctx, args)
	}
	return tools.ToolResult{}, nil
}

func (m *mockAgentGateway) ReadImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if m.readImageFunc != nil {
		return m.readImageFunc(ctx, args)
	}
	return tools.ToolResult{}, nil
}

func TestMediaTools(t *testing.T) {
	ctx := context.Background()
	r := registry.New()
	sm := security.NewSecurityManager(nil)
	gateway := &mockAgentGateway{
		generateImageFunc: func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "image generated"}, nil
		},
		readImageFunc: func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "image read"}, nil
		},
	}

	Register(r, sm, gateway)

	// Test create_image
	res, err := r.Execute(ctx, "create_image", map[string]interface{}{"prompt": "a sunset"})
	if err != nil {
		t.Fatalf("create_image failed: %v", err)
	}
	if res.Text != "image generated" {
		t.Errorf("expected image generated, got %s", res.Text)
	}

	// Test read_image
	res, err = r.Execute(ctx, "read_image", map[string]interface{}{"filepath": "test.png"})
	if err != nil {
		t.Fatalf("read_image failed: %v", err)
	}
	if res.Text != "image read" {
		t.Errorf("expected image read, got %s", res.Text)
	}
}

func TestMediaTools_NoGateway(t *testing.T) {
	ctx := context.Background()
	r := registry.New()
	sm := security.NewSecurityManager(nil)

	Register(r, sm, nil)

	// Test create_image
	_, err := r.Execute(ctx, "create_image", map[string]interface{}{"prompt": "a sunset"})
	if !errors.Is(err, tools.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}

	// Test read_image
	_, err = r.Execute(ctx, "read_image", map[string]interface{}{"filepath": "test.png"})
	if !errors.Is(err, tools.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

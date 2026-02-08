// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package media

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type mediaManager struct {
	sm           *security.SecurityManager
	agentGateway tools.AgentGateway
}

// Register adds media generation tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager, gateway tools.AgentGateway) {
	m := &mediaManager{sm: sm, agentGateway: gateway}

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "create_image",
		Description: "Generates an image from a text prompt using an Imagen model (default: imagen-3.0-generate-001). Saves to assets/generated/.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"prompt": {
					Type:        "STRING",
					Description: "Detailed description of the image to generate.",
				},
				"aspect_ratio": {
					Type:        "STRING",
					Description: "Aspect ratio (e.g., '1:1', '4:3', '16:9'). Default '1:1'.",
				},
				"model": {
					Type:        "STRING",
					Description: "The model to use for generation (e.g., 'imagen-3.0-generate-001', 'imagen-3.0-fast-001').",
				},
			},
			Required: []string{"prompt"},
		},
	}, m.createImage, registry.ToolOptions{LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "read_image",
		Description: "Reads a local image file for vision analysis.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the image file (e.g., './assets/screenshot.png').",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.readImage)
}

func (m *mediaManager) createImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if m.agentGateway == nil {
		return tools.ToolResult{}, tools.ErrNotImplemented
	}
	return m.agentGateway.GenerateImage(ctx, args)
}

func (m *mediaManager) readImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	if m.agentGateway == nil {
		return tools.ToolResult{}, tools.ErrNotImplemented
	}
	return m.agentGateway.ReadImage(ctx, args)
}

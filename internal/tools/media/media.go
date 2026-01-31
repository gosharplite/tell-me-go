// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package media

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type mediaManager struct {
	sm           *security.SecurityManager
	agentGateway types.AgentGateway
}

// Register adds media generation tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager, gateway types.AgentGateway) {
	m := &mediaManager{sm: sm, agentGateway: gateway}

	r.Register(&types.ToolDeclaration{
		Name:        "create_image",
		Description: "Generates an image from a text prompt using an Imagen model (default: imagen-3.0-generate-001). Saves to assets/generated/.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
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
	}, m.createImage)

	r.Register(&types.ToolDeclaration{
		Name:        "read_image",
		Description: "Reads a local image file for vision analysis.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the image file (e.g., './assets/screenshot.png').",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.readImage)
}

func (m *mediaManager) createImage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	if m.agentGateway == nil {
		return types.ToolResult{}, types.ErrNotImplemented
	}
	return m.agentGateway.GenerateImage(ctx, args)
}

func (m *mediaManager) readImage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	if m.agentGateway == nil {
		return types.ToolResult{}, types.ErrNotImplemented
	}
	return m.agentGateway.ReadImage(ctx, args)
}

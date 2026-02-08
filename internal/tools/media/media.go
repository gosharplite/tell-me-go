// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type mediaManager struct {
	sm        *security.SecurityManager
	client    llm.LLMClient
	assetsDir string
}

// Register adds media generation tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager, client llm.LLMClient, assetsDir string) {
	m := &mediaManager{sm: sm, client: client, assetsDir: assetsDir}

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
	if m.client == nil {
		return tools.ToolResult{}, tools.ErrNotImplemented
	}

	var a struct {
		Prompt      string `json:"prompt"`
		AspectRatio string `json:"aspect_ratio"`
		Model       string `json:"model"`
	}
	if err := tools.UnmarshalArgs(args, &a); err != nil {
		return tools.ToolResult{}, err
	}

	if a.Model == "" {
		a.Model = "imagen-3.0-generate-001"
	}

	prompt := a.Prompt
	if a.AspectRatio != "" {
		prompt = fmt.Sprintf("%s (aspect ratio %s)", prompt, a.AspectRatio)
	}

	images, err := m.client.GenerateImages(ctx, a.Model, prompt, "image/png")
	if err != nil {
		return tools.ToolResult{}, err
	}

	result := tools.ToolResult{
		Text: fmt.Sprintf("Generated %d images for prompt: %s", len(images), a.Prompt),
	}
	for i, data := range images {
		result.BinaryData = append(result.BinaryData, tools.BinaryData{
			MIMEType: "image/png",
			Data:     data,
		})
		// Auto-save to assetsDir
		if m.assetsDir != "" {
			filename := filepath.Join(m.assetsDir, fmt.Sprintf("image_%d_%d.png", time.Now().Unix(), i))
			_ = os.MkdirAll(m.assetsDir, 0755)
			_ = os.WriteFile(filename, data, 0644)
			result.Text += fmt.Sprintf("\nSaved to %s", filename)
		}
	}

	return result, nil
}

func (m *mediaManager) readImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var a struct {
		Filepath string `json:"filepath"`
	}
	if err := tools.UnmarshalArgs(args, &a); err != nil {
		return tools.ToolResult{}, err
	}

	data, err := os.ReadFile(a.Filepath)
	if err != nil {
		return tools.ToolResult{}, err
	}

	mimeType := "image/png"
	ext := strings.ToLower(filepath.Ext(a.Filepath))
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".webp":
		mimeType = "image/webp"
	}

	return tools.ToolResult{
		Text: fmt.Sprintf("Successfully read image from %s", a.Filepath),
		BinaryData: []tools.BinaryData{
			{
				MIMEType: mimeType,
				Data:     data,
			},
		},
	}, nil
}

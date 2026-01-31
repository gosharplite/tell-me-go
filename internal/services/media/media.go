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

	"github.com/gosharplite/tell-me-go/internal/types"
)

// Service handles image generation and storage.
type Service struct {
	client types.LLMClient
}

// NewService creates a new media service.
func NewService(client types.LLMClient) *Service {
	return &Service{
		client: client,
	}
}

// GenerateImage handles image generation requests and saves them to disk.
func (s *Service) GenerateImage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var a struct {
		Prompt      string `json:"prompt"`
		AspectRatio string `json:"aspect_ratio"`
		Model       string `json:"model"`
	}
	if err := types.UnmarshalArgs(args, &a); err != nil {
		return types.ToolResult{}, err
	}

	if a.Model == "" {
		a.Model = "imagen-3.0-generate-001"
	}

	prompt := a.Prompt
	if a.AspectRatio != "" {
		prompt = fmt.Sprintf("%s (aspect ratio %s)", prompt, a.AspectRatio)
	}

	images, err := s.client.GenerateImages(ctx, a.Model, prompt, "image/png")
	if err != nil {
		return types.ToolResult{}, err
	}

	result := types.ToolResult{
		Text: fmt.Sprintf("Generated %d images for prompt: %s", len(images), a.Prompt),
	}
	for i, data := range images {
		result.BinaryData = append(result.BinaryData, types.BinaryData{
			MIMEType: "image/png",
			Data:     data,
		})
		// Auto-save to assets/generated
		filename := fmt.Sprintf("assets/generated/image_%d_%d.png", time.Now().Unix(), i)
		_ = os.MkdirAll("assets/generated", 0755)
		_ = os.WriteFile(filename, data, 0644)
		result.Text += fmt.Sprintf("\nSaved to %s", filename)
	}

	return result, nil
}

// ReadImage reads an image from the filesystem.
func (s *Service) ReadImage(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var a struct {
		Filepath string `json:"filepath"`
	}
	if err := types.UnmarshalArgs(args, &a); err != nil {
		return types.ToolResult{}, err
	}

	data, err := os.ReadFile(a.Filepath)
	if err != nil {
		return types.ToolResult{}, err
	}

	mimeType := "image/png"
	ext := strings.ToLower(filepath.Ext(a.Filepath))
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".webp":
		mimeType = "image/webp"
	}

	return types.ToolResult{
		Text: fmt.Sprintf("Successfully read image from %s", a.Filepath),
		BinaryData: []types.BinaryData{
			{
				MIMEType: mimeType,
				Data:     data,
			},
		},
	}, nil
}

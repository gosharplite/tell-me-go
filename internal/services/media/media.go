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
)

// Service handles image generation and storage.
type Service struct {
	client    llm.LLMClient
	assetsDir string
}

// NewService creates a new media service.
func NewService(client llm.LLMClient, assetsDir string) *Service {
	return &Service{
		client:    client,
		assetsDir: assetsDir,
	}
}

// GenerateImage handles image generation requests and saves them to disk.
func (s *Service) GenerateImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	images, err := s.client.GenerateImages(ctx, a.Model, prompt, "image/png")
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
		if s.assetsDir != "" {
			filename := filepath.Join(s.assetsDir, fmt.Sprintf("image_%d_%d.png", time.Now().Unix(), i))
			_ = os.MkdirAll(s.assetsDir, 0755)
			_ = os.WriteFile(filename, data, 0644)
			result.Text += fmt.Sprintf("\nSaved to %s", filename)
		}
	}

	return result, nil
}

// ReadImage reads an image from the filesystem.
func (s *Service) ReadImage(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

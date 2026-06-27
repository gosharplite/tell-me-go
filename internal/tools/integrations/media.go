// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/telemetry"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

type mediaOption func(*mediaManager)

func withMediaHeartbeatInterval(d time.Duration) mediaOption {
	return func(m *mediaManager) {
		m.heartbeatInterval = d
	}
}

func newMediaManager(fs persistence.FileSystem, sm security.PathValidator, client llm.LLMClient, assetsDir string, opts ...mediaOption) *mediaManager {
	m := &mediaManager{
		fs:                fs,
		sm:                sm,
		client:            client,
		assetsDir:         assetsDir,
		heartbeatInterval: 2 * time.Second, // Default
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

type mediaManager struct {
	fs                persistence.FileSystem
	sm                security.PathValidator
	client            llm.LLMClient
	assetsDir         string
	heartbeatInterval time.Duration
}

type imageRequest struct {
	Prompt      string
	AspectRatio string
	Model       string
}

func (m *mediaManager) createImage(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if m.client == nil {
		return tools.ToolResult{}, tools.ErrNotImplemented
	}

	req, err := m.parseImageArgs(args)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("parse image args: %w", err)
	}

	prompt := req.Prompt
	if req.AspectRatio != "" {
		prompt = fmt.Sprintf("%s (aspect ratio %s)", prompt, req.AspectRatio)
	}

	stop := telemetry.StartHeartbeat(ctx, m.heartbeatInterval, hb)
	defer stop()

	images, err := m.client.GenerateImages(ctx, req.Model, prompt, "image/png")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("generate images: %w", err)
	}

	return m.saveImagesToDisk(ctx, images, req.Prompt)
}

func (m *mediaManager) parseImageArgs(args map[string]interface{}) (*imageRequest, error) {
	var a struct {
		Prompt      string `json:"prompt"`
		AspectRatio string `json:"aspect_ratio"`
		Model       string `json:"model"`
	}
	if err := tools.UnmarshalArgs(args, &a); err != nil {
		return nil, fmt.Errorf("unmarshal args: %w", err)
	}

	if a.Model == "" {
		a.Model = "imagen-3.0-generate-001"
	}

	return &imageRequest{
		Prompt:      a.Prompt,
		AspectRatio: a.AspectRatio,
		Model:       a.Model,
	}, nil
}

func (m *mediaManager) saveImagesToDisk(ctx context.Context, images [][]byte, prompt string) (tools.ToolResult, error) {
	result := tools.ToolResult{
		Text: fmt.Sprintf("Generated %d images for prompt: %s", len(images), prompt),
	}
	for i, data := range images {
		result.BinaryData = append(result.BinaryData, tools.BinaryData{
			MIMEType: "image/png",
			Data:     data,
		})
		// Auto-save to assetsDir
		if m.assetsDir != "" {
			filename := filepath.Join(m.assetsDir, fmt.Sprintf("image_%d_%d.png", time.Now().Unix(), i))
			if err := m.fs.MkdirAll(ctx, m.assetsDir, 0755); err != nil {
				return tools.ToolResult{}, fmt.Errorf("create assets directory: %w", err)
			}
			if err := m.fs.AtomicWrite(ctx, filename, data, 0644); err != nil {
				return tools.ToolResult{}, fmt.Errorf("write image file %s: %w", filename, err)
			}
			result.Text += fmt.Sprintf("\nSaved to %s", filename)
		}
	}
	return result, nil
}

func (m *mediaManager) readImage(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	// readImage is a synchronous read operation (not registered as LongRunning),
	// so heartbeat is intentionally not started. The hb parameter is required by
	// the tools.ToolFunc interface signature.
	_ = hb

	var a struct {
		Filepath string `json:"filepath"`
	}
	if err := tools.UnmarshalArgs(args, &a); err != nil {
		return tools.ToolResult{}, fmt.Errorf("unmarshal args: %w", err)
	}

	if m.sm == nil {
		return tools.ToolResult{}, fmt.Errorf("path validator is required")
	}

	safePath, err := m.sm.IsPathSafe(a.Filepath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("security validation failed for path %s: %w", a.Filepath, err)
	}

	data, err := m.fs.ReadFile(ctx, safePath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("read file %s: %w", safePath, err)
	}

	mimeType := "image/png"
	ext := strings.ToLower(filepath.Ext(safePath))
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".webp":
		mimeType = "image/webp"
	}

	return tools.ToolResult{
		Text: fmt.Sprintf("Successfully read image from %s", safePath),
		BinaryData: []tools.BinaryData{
			{
				MIMEType: mimeType,
				Data:     data,
			},
		},
	}, nil
}

func registerMedia(r tools.Registry, fs persistence.FileSystem, sm security.Manager, client llm.LLMClient, assetsDir string) error {
	m := newMediaManager(fs, sm, client, assetsDir)

	if err := r.RegisterToToolkitWithOptions("media", &tools.ToolDeclaration{
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
	}, m.createImage, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterToToolkit("media", &tools.ToolDeclaration{
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
	}, m.readImage); err != nil {
		return err
	}
	return nil
}

// mediaPlugin implements plugin.Plugin for the media toolkit.
type mediaPlugin struct{}

func (mediaPlugin) Name() string { return "media" }

func (mediaPlugin) Register(r tools.Registry, deps plugin.PluginDependencies) error {
	return registerMedia(r, deps.FileSystem, deps.SecurityMgr, deps.LLMClient, deps.AssetsDir)
}

func init() {
	_ = plugin.Register(&mediaPlugin{}) //nolint:errcheck // init() cannot return errors; duplicate names caught in tests
}

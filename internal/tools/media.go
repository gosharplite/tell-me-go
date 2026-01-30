// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/api"
	"google.golang.org/genai"
)

type mediaManager struct {
	sm     *SecurityManager
	client *api.Client
}

// RegisterMediaTools adds image and media-related tools to the registry.
func RegisterMediaTools(r *Registry, sm *SecurityManager, client *api.Client) {
	m := &mediaManager{sm: sm, client: client}

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "create_image",
		Description: "Generates an image from a text prompt using an Imagen model (default: imagen-3.0-generate-001). Saves to assets/generated/.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"prompt": {
					Type:        genai.TypeString,
					Description: "Detailed description of the image to generate.",
				},
				"aspect_ratio": {
					Type:        genai.TypeString,
					Description: "Aspect ratio (e.g., '1:1', '4:3', '16:9'). Default '1:1'.",
				},
				"model": {
					Type:        genai.TypeString,
					Description: "The model to use for generation (e.g., 'imagen-3.0-generate-001', 'imagen-3.0-fast-001').",
				},
			},
			Required: []string{"prompt"},
		},
	}, m.createImage, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "read_image",
		Description: "Reads a local image file for vision analysis.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"filepath": {
					Type:        genai.TypeString,
					Description: "The path to the image file (e.g., './assets/screenshot.png').",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.readImage)
}

func (m *mediaManager) createImage(ctx context.Context, args map[string]interface{}) (string, error) {
	prompt, _ := args["prompt"].(string)
	aspectRatio, _ := args["aspect_ratio"].(string)
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}

	model, _ := args["model"].(string)
	if model == "" {
		model = "imagen-3.0-generate-001"
	}

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Generating image using %s: %s (%s)\033[0m\n", model, prompt, aspectRatio)
	}()

	// Append aspect ratio to prompt as guidance (Imagen 3 prompt engineering)
	fullPrompt := fmt.Sprintf("%s. Aspect ratio %s.", prompt, aspectRatio)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Use specified model
	images, err := m.client.GenerateImages(ctx, model, fullPrompt, "image/png")
	if err != nil {
		return fmt.Sprintf("Error generating image: %v", err), nil
	}

	if len(images) == 0 {
		return "Error: No images were generated.", nil
	}

	// Create directory
	outDir := "assets/generated"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Sprintf("Error creating output directory: %v", err), nil
	}

	// Save first image
	timestamp := time.Now().Format("20060102_150405")
	safePrompt := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, prompt)
	if len(safePrompt) > 30 {
		safePrompt = safePrompt[:30]
	}

	filename := fmt.Sprintf("%s_%s.png", timestamp, safePrompt)
	outPath := filepath.Join(outDir, filename)

	if err := os.WriteFile(outPath, images[0], 0644); err != nil {
		return fmt.Sprintf("Error saving image: %v", err), nil
	}

	return fmt.Sprintf("Image generated successfully and saved to: %s", outPath), nil
}

func (m *mediaManager) readImage(ctx context.Context, args map[string]interface{}) (string, error) {
	path, _ := args["filepath"].(string)
	if err := m.sm.IsPathSafe(path); err != nil {
		return "", err
	}

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] Reading image for vision: %s\033[0m\n", path)
	}()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err), nil
	}

	// Detect MIME type
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return fmt.Sprintf("Error: File is not a supported image (detected: %s)", mimeType), nil
	}

	b64Data := base64.StdEncoding.EncodeToString(data)

	// Special prefix that the agent will catch
	return fmt.Sprintf("MULTI_MODAL_IMAGE|%s|%s|Successfully read image %s. You can now see it.", mimeType, b64Data, path), nil
}

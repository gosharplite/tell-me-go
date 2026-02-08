// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type mockLLMClient struct {
	llm.LLMClient
	generateImagesFunc func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	if m.generateImagesFunc != nil {
		return m.generateImagesFunc(ctx, model, prompt, mimeType)
	}
	return nil, nil
}

func TestMediaTools(t *testing.T) {
	ctx := context.Background()
	r := registry.New()
	sm := security.NewSecurityManager(nil)
	client := &mockLLMClient{
		generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
			return [][]byte{[]byte("fake-image")}, nil
		},
	}

	Register(r, sm, client, t.TempDir())

	// Test create_image
	res, err := r.Execute(ctx, "create_image", map[string]interface{}{"prompt": "a sunset"})
	if err != nil {
		t.Fatalf("create_image failed: %v", err)
	}
	if len(res.BinaryData) == 0 {
		t.Errorf("expected binary data, got none")
	}

	// Test read_image - need a real file
	tmpFile := filepath.Join(t.TempDir(), "test.png")
	_ = os.WriteFile(tmpFile, []byte("fake-image"), 0644)

	res, err = r.Execute(ctx, "read_image", map[string]interface{}{"filepath": tmpFile})
	if err != nil {
		t.Fatalf("read_image failed: %v", err)
	}
	if len(res.BinaryData) == 0 {
		t.Errorf("expected binary data, got none")
	}
}

func TestMediaTools_NoClient(t *testing.T) {
	ctx := context.Background()
	r := registry.New()
	sm := security.NewSecurityManager(nil)

	Register(r, sm, nil, "")

	// Test create_image
	_, err := r.Execute(ctx, "create_image", map[string]interface{}{"prompt": "a sunset"})
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

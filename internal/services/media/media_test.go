// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package media

import (
	"context"
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/types"
)

type mockLLMClient struct {
	types.LLMClient
	generateImagesFn func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	if m.generateImagesFn != nil {
		return m.generateImagesFn(ctx, model, prompt, mimeType)
	}
	return nil, nil
}

func TestMediaService_GenerateImage(t *testing.T) {
	mockClient := &mockLLMClient{
		generateImagesFn: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
			return [][]byte{[]byte("fake-image-data")}, nil
		},
	}
	s := NewService(mockClient)

	args := map[string]interface{}{
		"prompt": "a test prompt",
	}
	result, err := s.GenerateImage(context.Background(), args)
	if err != nil {
		t.Fatalf("GenerateImage failed: %v", err)
	}

	if len(result.BinaryData) != 1 {
		t.Errorf("Expected 1 binary data, got %d", len(result.BinaryData))
	}

	if result.BinaryData[0].MIMEType != "image/png" {
		t.Errorf("Expected MIME type image/png, got %s", result.BinaryData[0].MIMEType)
	}

	// Clean up generated assets if any
	_ = os.RemoveAll("assets/generated")
}

func TestMediaService_ReadImage(t *testing.T) {
	s := NewService(nil)

	tmpFile := "test_image.png"
	err := os.WriteFile(tmpFile, []byte("fake-image-data"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(tmpFile)

	args := map[string]interface{}{
		"filepath": tmpFile,
	}
	result, err := s.ReadImage(context.Background(), args)
	if err != nil {
		t.Fatalf("ReadImage failed: %v", err)
	}

	if len(result.BinaryData) != 1 {
		t.Errorf("Expected 1 binary data, got %d", len(result.BinaryData))
	}

	if string(result.BinaryData[0].Data) != "fake-image-data" {
		t.Errorf("Expected data 'fake-image-data', got %s", string(result.BinaryData[0].Data))
	}

	if result.BinaryData[0].MIMEType != "image/png" {
		t.Errorf("Expected MIME type image/png, got %s", result.BinaryData[0].MIMEType)
	}
}

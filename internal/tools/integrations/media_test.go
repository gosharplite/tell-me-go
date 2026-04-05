// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	infra_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"go.uber.org/goleak"
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

type mediaMockSecurityManager struct {
	domain_security.Manager
	isPathSafeFunc func(path string) (string, error)
}

func (m *mediaMockSecurityManager) IsPathSafe(path string) (string, error) {
	if m.isPathSafeFunc != nil {
		return m.isPathSafeFunc(path)
	}
	return m.Manager.IsPathSafe(path)
}

func newMediaMockSecurityManager() *mediaMockSecurityManager {
	return &mediaMockSecurityManager{
		Manager: infra_security.NewSecurityManager(nil),
	}
}

func TestMediaTools_CreateImage(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	ctx := context.Background()
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		args       map[string]interface{}
		client     *mockLLMClient
		assetsDir  string
		wantImages int
		wantErr    bool
	}{
		{
			name: "successful generation with default model",
			args: map[string]interface{}{"prompt": "a sunset"},
			client: &mockLLMClient{
				generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
					if model != "imagen-3.0-generate-001" {
						return nil, fmt.Errorf("wrong model: %s", model)
					}
					return [][]byte{[]byte("fake-image")}, nil
				},
			},
			wantImages: 1,
		},
		{
			name: "successful generation with custom model and aspect ratio",
			args: map[string]interface{}{
				"prompt":       "a sunset",
				"model":        "custom-model",
				"aspect_ratio": "16:9",
			},
			client: &mockLLMClient{
				generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
					if model != "custom-model" {
						return nil, fmt.Errorf("wrong model: %s", model)
					}
					if !strings.Contains(prompt, "aspect ratio 16:9") {
						return nil, fmt.Errorf("prompt missing aspect ratio: %s", prompt)
					}
					return [][]byte{[]byte("img1"), []byte("img2")}, nil
				},
			},
			wantImages: 2,
		},
		{
			name: "generate images failure",
			args: map[string]interface{}{"prompt": "fail"},
			client: &mockLLMClient{
				generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
					return nil, errors.New("API error")
				},
			},
			wantErr: true,
		},
		{
			name:      "save to disk success",
			args:      map[string]interface{}{"prompt": "save"},
			assetsDir: tmpDir,
			client: &mockLLMClient{
				generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
					return [][]byte{[]byte("data")}, nil
				},
			},
			wantImages: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMediaMockSecurityManager()

			// Capture the mediaManager and set a short heartbeat interval for fast tests
			m := newMediaManager(sm, tt.client, tt.assetsDir, WithMediaHeartbeatInterval(10*time.Millisecond))

			res, err := m.createImage(ctx, tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("createImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(res.BinaryData) != tt.wantImages {
					t.Errorf("got %d images, want %d", len(res.BinaryData), tt.wantImages)
				}
				if tt.assetsDir != "" {
					files, _ := os.ReadDir(tt.assetsDir)
					found := false
					for _, f := range files {
						if strings.HasPrefix(f.Name(), "image_") && strings.HasSuffix(f.Name(), ".png") {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected image file to be saved in %s", tt.assetsDir)
					}
				}
			}
		})
	}
}

func TestMediaTools_ReadImage(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create some test files
	pngFile := filepath.Join(tmpDir, "test.png")
	_ = os.WriteFile(pngFile, []byte("png-data"), 0644)

	jpgFile := filepath.Join(tmpDir, "test.jpg")
	_ = os.WriteFile(jpgFile, []byte("jpg-data"), 0644)

	// Create a directory
	subDir := filepath.Join(tmpDir, "subdir")
	_ = os.Mkdir(subDir, 0755)

	// Create a write-only file (on Unix-like systems)
	unreadableFile := filepath.Join(tmpDir, "unreadable.png")
	_ = os.WriteFile(unreadableFile, []byte("secret"), 0200)

	tests := []struct {
		name         string
		filepath     string
		isPathSafe   bool
		wantMimeType string
		wantErr      bool
	}{
		{
			name:         "read valid png",
			filepath:     pngFile,
			isPathSafe:   true,
			wantMimeType: "image/png",
		},
		{
			name:         "read valid jpg",
			filepath:     jpgFile,
			isPathSafe:   true,
			wantMimeType: "image/jpeg",
		},
		{
			name:       "security violation",
			filepath:   "/etc/passwd",
			isPathSafe: false,
			wantErr:    true,
		},
		{
			name:       "file not found",
			filepath:   filepath.Join(tmpDir, "nonexistent.png"),
			isPathSafe: true,
			wantErr:    true,
		},
		{
			name:       "read directory failure",
			filepath:   subDir,
			isPathSafe: true,
			wantErr:    true,
		},
		{
			name:       "permission denied failure",
			filepath:   unreadableFile,
			isPathSafe: true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMediaMockSecurityManager()
			sm.isPathSafeFunc = func(path string) (string, error) {
				if !tt.isPathSafe {
					return "", errors.New("unsafe path")
				}
				return path, nil
			}
			m := newMediaManager(sm, nil, "")

			res, err := m.readImage(ctx, map[string]interface{}{"filepath": tt.filepath}, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("readImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(res.BinaryData) != 1 {
					t.Fatalf("got %d images, want 1", len(res.BinaryData))
				}
				if res.BinaryData[0].MIMEType != tt.wantMimeType {
					t.Errorf("got MIME %s, want %s", res.BinaryData[0].MIMEType, tt.wantMimeType)
				}
			}
		})
	}
}

func TestNewMediaManager(t *testing.T) {
	sm := newMediaMockSecurityManager()
	client := &mockLLMClient{}
	assetsDir := "test-assets"
	interval := 5 * time.Second

	m := newMediaManager(sm, client, assetsDir, WithMediaHeartbeatInterval(interval))

	if m.sm != sm {
		t.Error("security manager not set correctly")
	}
	if m.client != client {
		t.Error("client not set correctly")
	}
	if m.assetsDir != assetsDir {
		t.Errorf("assetsDir = %s, want %s", m.assetsDir, assetsDir)
	}
	if m.heartbeatInterval != interval {
		t.Errorf("heartbeatInterval = %v, want %v", m.heartbeatInterval, interval)
	}
}

func TestMediaManager_DefaultOptions(t *testing.T) {
	m := newMediaManager(nil, nil, "")
	if m.heartbeatInterval != 2*time.Second {
		t.Errorf("default heartbeatInterval = %v, want 2s", m.heartbeatInterval)
	}
}

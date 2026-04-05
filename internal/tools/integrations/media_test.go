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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
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
			r := registry.New()
			sm := newMediaMockSecurityManager()
			if err := RegisterAll(r, sm, tt.client, tt.assetsDir); err != nil {
				t.Fatalf("RegisterAll failed: %v", err)
			}

			// Capture the mediaManager and set a short heartbeat interval for fast tests
			// In a real scenario, RegisterAll might need to be more flexible, but for tests we can reach in
			// if we have access to the registry's internals or just use a helper.
			// Since RegisterAll creates it internally, we'll use a hack or just test the manager directly.
			m := &mediaManager{
				sm:                sm,
				client:            tt.client,
				assetsDir:         tt.assetsDir,
				heartbeatInterval: 10 * time.Millisecond,
			}

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
			m := &mediaManager{sm: sm}

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

func TestMediaTools_StartHeartbeat(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	hbInterval := 10 * time.Millisecond
	m := &mediaManager{heartbeatInterval: hbInterval}
	hb := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := m.startHeartbeat(ctx, hb)

	// Ensure heartbeat is working and emits multiple signals quickly
	for i := 0; i < 3; i++ {
		select {
		case <-hb:
			// Heartbeat received
		case <-time.After(100 * time.Millisecond):
			t.Errorf("timeout waiting for heartbeat signal %d", i)
		}
	}

	// Test idempotent stop
	stop()
	stop() // Should not panic

	// Ensure no more heartbeats after stop
	time.Sleep(20 * time.Millisecond)
	select {
	case <-hb:
		t.Error("received heartbeat after stop")
	default:
	}
}

func TestMediaTools_StartHeartbeat_Cancellation(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	hbInterval := 10 * time.Millisecond
	m := &mediaManager{heartbeatInterval: hbInterval}
	hb := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())

	_ = m.startHeartbeat(ctx, hb)
	cancel()

	// Ensure no more heartbeats after cancellation
	time.Sleep(20 * time.Millisecond)
	select {
	case <-hb:
		t.Error("received heartbeat after cancellation")
	default:
	}
}

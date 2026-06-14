// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
	"go.uber.org/goleak"
)

type mockFileSystem struct {
	persistence.FileSystem
}

func (m *mockFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (m *mockFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (m *mockFileSystem) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (m *mockFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (m *mockFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return os.Stat(name)
}

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
		Manager: &toolstest.MockSecurityManager{AllowAll: true},
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
			fs := &mockFileSystem{}

			// Capture the mediaManager and set a short heartbeat interval for fast tests
			m := newMediaManager(fs, sm, tt.client, tt.assetsDir, withMediaHeartbeatInterval(10*time.Millisecond))

			res, err := m.createImage(ctx, tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("createImage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				verifyImageCreation(t, res, tt.assetsDir, tt.wantImages)
			}
		})
	}
}

func verifyImageCreation(t *testing.T, res tools.ToolResult, assetsDir string, wantImages int) {
	t.Helper()
	if len(res.BinaryData) != wantImages {
		t.Errorf("got %d images, want %d", len(res.BinaryData), wantImages)
	}
	if assetsDir != "" {
		files, _ := os.ReadDir(assetsDir)
		found := false
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "image_") && strings.HasSuffix(f.Name(), ".png") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected image file to be saved in %s", assetsDir)
		}
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

	webpFile := filepath.Join(tmpDir, "test.webp")
	_ = os.WriteFile(webpFile, []byte("webp-data"), 0644)
	bmpFile := filepath.Join(tmpDir, "test.bmp")
	_ = os.WriteFile(bmpFile, []byte("bmp-data"), 0644)

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
			name:         "read webp",
			filepath:     webpFile,
			isPathSafe:   true,
			wantMimeType: "image/webp",
		},
		{
			name:         "read unknown extension defaults to png",
			filepath:     bmpFile,
			isPathSafe:   true,
			wantMimeType: "image/png",
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
			if tt.name == "permission denied failure" && runtime.GOOS == "windows" {
				t.Skip("Skipping on Windows: os.Chmod(0200) does not reliably make files unreadable for the owner.")
			}
			sm := newMediaMockSecurityManager()
			sm.isPathSafeFunc = func(path string) (string, error) {
				if !tt.isPathSafe {
					return "", errors.New("unsafe path")
				}
				return path, nil
			}
			fs := &mockFileSystem{}
			m := newMediaManager(fs, sm, nil, "")

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
	fs := &mockFileSystem{}
	client := &mockLLMClient{}
	assetsDir := "test-assets"
	interval := 5 * time.Second

	m := newMediaManager(fs, sm, client, assetsDir, withMediaHeartbeatInterval(interval))

	if m.fs != fs {
		t.Error("filesystem not set correctly")
	}
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
	m := newMediaManager(nil, nil, nil, "")
	if m.heartbeatInterval != 2*time.Second {
		t.Errorf("default heartbeatInterval = %v, want 2s", m.heartbeatInterval)
	}
}

// mkdirFailFS overrides MkdirAll to simulate a filesystem that cannot create directories.
type mkdirFailFS struct{ *mockFileSystem }

func (m *mkdirFailFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return errors.New("mkdir failed")
}

// atomicWriteFailFS overrides AtomicWrite to simulate a write failure after directory creation succeeds.
type atomicWriteFailFS struct{ *mockFileSystem }

func (m *atomicWriteFailFS) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}

func (m *atomicWriteFailFS) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return errors.New("write failed")
}

func TestMediaTools_ErrorPaths(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
	ctx := context.Background()
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		setup       func() *mediaManager
		action      func(m *mediaManager) error
		errText     string
		errSentinel error
	}{
		{
			name: "nil LLM client returns ErrNotImplemented",
			setup: func() *mediaManager {
				return newMediaManager(&mockFileSystem{}, newMediaMockSecurityManager(), nil, "")
			},
			action: func(m *mediaManager) error {
				_, err := m.createImage(ctx, map[string]interface{}{"prompt": "test"}, nil)
				return err
			},
			errSentinel: tools.ErrNotImplemented,
		},
		{
			name: "parseImageArgs failure on invalid args",
			setup: func() *mediaManager {
				return newMediaManager(&mockFileSystem{}, newMediaMockSecurityManager(), &mockLLMClient{}, "")
			},
			action: func(m *mediaManager) error {
				_, err := m.createImage(ctx, map[string]interface{}{"prompt": 123}, nil)
				return err
			},
			errText: "unmarshal args",
		},
		{
			name: "MkdirAll failure in saveImagesToDisk",
			setup: func() *mediaManager {
				client := &mockLLMClient{
					generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
						return [][]byte{[]byte("data")}, nil
					},
				}
				return newMediaManager(
					&mkdirFailFS{&mockFileSystem{}},
					newMediaMockSecurityManager(),
					client,
					"/nonexistent/path",
				)
			},
			action: func(m *mediaManager) error {
				_, err := m.createImage(ctx, map[string]interface{}{"prompt": "test"}, nil)
				return err
			},
			errText: "create assets directory",
		},
		{
			name: "AtomicWrite failure in saveImagesToDisk",
			setup: func() *mediaManager {
				client := &mockLLMClient{
					generateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
						return [][]byte{[]byte("data")}, nil
					},
				}
				return newMediaManager(
					&atomicWriteFailFS{&mockFileSystem{}},
					newMediaMockSecurityManager(),
					client,
					tmpDir,
				)
			},
			action: func(m *mediaManager) error {
				_, err := m.createImage(ctx, map[string]interface{}{"prompt": "test"}, nil)
				return err
			},
			errText: "write image file",
		},
		{
			name: "nil security manager in readImage",
			setup: func() *mediaManager {
				return newMediaManager(&mockFileSystem{}, nil, &mockLLMClient{}, "")
			},
			action: func(m *mediaManager) error {
				_, err := m.readImage(ctx, map[string]interface{}{"filepath": "test.png"}, nil)
				return err
			},
			errText: "path validator is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.setup()
			err := tt.action(m)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.errSentinel != nil && !errors.Is(err, tt.errSentinel) {
				t.Errorf("expected error %v, got %v", tt.errSentinel, err)
			}
			if tt.errText != "" && !strings.Contains(err.Error(), tt.errText) {
				t.Errorf("expected error to contain %q, got %q", tt.errText, err.Error())
			}
		})
	}
}

// faultyRegistry wraps a real registry and forces a registration error after failOn
// successful registrations. Used to test error propagation through register* functions.
type faultyRegistry struct {
	tools.Registry
	failOn int
	count  int
}

func newFaultyRegistry(real tools.Registry, failOn int) *faultyRegistry {
	return &faultyRegistry{Registry: real, failOn: failOn}
}

func (r *faultyRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	r.count++
	if r.count > r.failOn {
		return errors.New("simulated registration failure")
	}
	return r.Registry.RegisterToToolkit(toolkit, def, handler)
}

func (r *faultyRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	r.count++
	if r.count > r.failOn {
		return errors.New("simulated registration failure")
	}
	return r.Registry.RegisterToToolkitWithOptions(toolkit, def, handler, opts)
}

func (r *faultyRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	r.count++
	if r.count > r.failOn {
		return errors.New("simulated registration failure")
	}
	return r.Registry.Register(def, handler)
}

func (r *faultyRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	r.count++
	if r.count > r.failOn {
		return errors.New("simulated registration failure")
	}
	return r.Registry.RegisterWithOptions(def, handler, opts)
}

// ── RegisterAll error propagation tests (Issue #433) ──

func TestRegisterAll_ErrorPropagation(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := &mockFileSystem{}
	client := &mockLLMClient{}

	// Step 1 (registerMedia first call) fails immediately
	t.Run("media registration fails", func(t *testing.T) {
		r := newFaultyRegistry(registry.New(), 0)
		err := RegisterAll(r, fs, sm, client, t.TempDir())
		if err == nil {
			t.Fatal("expected error from RegisterAll when media registration fails")
		}
		if !strings.Contains(err.Error(), "simulated registration failure") {
			t.Errorf("expected simulated error, got: %v", err)
		}
	})

	// registerMedia succeeds (2 calls), registerNetwork first call fails (call #3)
	t.Run("network registration fails", func(t *testing.T) {
		r := newFaultyRegistry(registry.New(), 2)
		err := RegisterAll(r, fs, sm, client, t.TempDir())
		if err == nil {
			t.Fatal("expected error from RegisterAll when network registration fails")
		}
		if !strings.Contains(err.Error(), "simulated registration failure") {
			t.Errorf("expected simulated error, got: %v", err)
		}
	})
}

func TestRegisterAll_ErrorWrapping(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	fs := &mockFileSystem{}
	client := &mockLLMClient{}
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		failAfter     int
		wantSubstring string
	}{
		{
			name:          "registerMedia wraps error",
			failAfter:     0,
			wantSubstring: "registerMedia",
		},
		{
			name:          "registerNetwork wraps error",
			failAfter:     2,
			wantSubstring: "registerNetwork",
		},
		{
			name:          "registerTeams wraps error",
			failAfter:     4,
			wantSubstring: "registerTeams",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFaultyRegistry(registry.New(), tt.failAfter)
			err := RegisterAll(r, fs, sm, client, tmpDir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSubstring) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantSubstring)
			}
		})
	}
}

func TestRegisterMedia_ErrorPaths(t *testing.T) {
	sm := newMediaMockSecurityManager()
	fs := &mockFileSystem{}
	client := &mockLLMClient{}
	tmpDir := t.TempDir()

	t.Run("first RegisterToToolkitWithOptions fails", func(t *testing.T) {
		r := newFaultyRegistry(registry.New(), 0)
		err := registerMedia(r, fs, sm, client, tmpDir)
		if err == nil {
			t.Fatal("expected error on first registration failure")
		}
		if !strings.Contains(err.Error(), "simulated registration failure") {
			t.Errorf("expected simulated error, got: %v", err)
		}
	})

	t.Run("second RegisterToToolkit fails", func(t *testing.T) {
		r := newFaultyRegistry(registry.New(), 1)
		err := registerMedia(r, fs, sm, client, tmpDir)
		if err == nil {
			t.Fatal("expected error on second registration failure")
		}
		if !strings.Contains(err.Error(), "simulated registration failure") {
			t.Errorf("expected simulated error, got: %v", err)
		}
	})
}

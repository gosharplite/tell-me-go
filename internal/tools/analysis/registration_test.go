// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRegistry implements tools.Registry minimally for testing Register errors.
type stubRegistry struct {
	registerErr            error
	registerWithOptionsErr error
}

func (s *stubRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return s.registerErr
}

func (s *stubRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return s.registerWithOptionsErr
}

func (s *stubRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (s *stubRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func (s *stubRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (s *stubRegistry) IsSerial(name string) bool      { return false }
func (s *stubRegistry) IsLongRunning(name string) bool { return false }

func (s *stubRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{}
}

func (s *stubRegistry) GetDeclarations() []*tools.ToolDeclaration     { return nil }
func (s *stubRegistry) GetCoreDeclarations() []*tools.ToolDeclaration { return nil }
func (s *stubRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}
func (s *stubRegistry) ListAvailableToolkits() []string { return nil }

// stubEventBus implements events.EventBus minimally.
type stubEventBus struct{}

func (s *stubEventBus) Publish(ctx context.Context, e events.Event) error { return nil }
func (s *stubEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (s *stubEventBus) Shutdown(ctx context.Context) error                { return nil }
func (s *stubEventBus) Flush(ctx context.Context) error                   { return nil }
func (s *stubEventBus) Listen(ctx context.Context) error                  { return nil }
func (s *stubEventBus) WaitStarted()                                      {}

// stubFileSystem implements persistence.FileSystem minimally.
type stubFileSystem struct{}

func (s *stubFileSystem) ReadDir(ctx context.Context, name string) ([]os.DirEntry, error) {
	return nil, nil
}
func (s *stubFileSystem) ReadFile(ctx context.Context, name string) ([]byte, error) { return nil, nil }
func (s *stubFileSystem) WriteFile(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return nil
}
func (s *stubFileSystem) AtomicWrite(ctx context.Context, name string, data []byte, perm os.FileMode) error {
	return nil
}
func (s *stubFileSystem) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return nil
}
func (s *stubFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return nil, nil
}
func (s *stubFileSystem) Open(ctx context.Context, name string) (persistence.File, error) {
	return nil, nil
}
func (s *stubFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (persistence.File, error) {
	return nil, nil
}
func (s *stubFileSystem) Remove(ctx context.Context, name string) error    { return nil }
func (s *stubFileSystem) RemoveAll(ctx context.Context, path string) error { return nil }
func (s *stubFileSystem) Walk(ctx context.Context, root string, fn persistence.WalkFunc) error {
	return nil
}

func (s *stubFileSystem) Chmod(ctx context.Context, name string, mode os.FileMode) error {
	return nil
}

// stubWorkspacePolicy implements services.WorkspacePolicy minimally.
type stubWorkspacePolicy struct{}

func (s *stubWorkspacePolicy) ShouldIgnoreDir(name string) bool  { return false }
func (s *stubWorkspacePolicy) ShouldIgnorePath(path string) bool { return false }

// Compile-time interface satisfaction checks.
var (
	_ tools.Registry           = (*stubRegistry)(nil)
	_ events.EventBus          = (*stubEventBus)(nil)
	_ persistence.FileSystem   = (*stubFileSystem)(nil)
	_ services.WorkspacePolicy = (*stubWorkspacePolicy)(nil)
	_ security.Manager         = (*mockSecurityProvider)(nil)
	_ tools.CommandExecutor    = (*mockExecutor)(nil)
)

func TestRegister_ErrorWrapping(t *testing.T) {
	t.Parallel()

	t.Run("RegisterWithOptions failure includes tool name", func(t *testing.T) {
		t.Parallel()

		r := &stubRegistry{
			registerWithOptionsErr: errors.New("simulated registration failure"),
		}
		sp := &mockSecurityProvider{}
		bus := &stubEventBus{}
		exec := &mockExecutor{}
		fs := &stubFileSystem{}
		wp := &stubWorkspacePolicy{}

		_, err := Register(r, sp, bus, exec, fs, wp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "register tool")
		// First tool with opts is "verify_architecture"
		assert.Contains(t, err.Error(), "verify_architecture")
		assert.Contains(t, err.Error(), "simulated registration failure")
	})

	t.Run("Register failure includes tool name", func(t *testing.T) {
		t.Parallel()

		r := &stubRegistry{
			registerErr: errors.New("simulated registration failure"),
		}
		sp := &mockSecurityProvider{}
		bus := &stubEventBus{}
		exec := &mockExecutor{}
		fs := &stubFileSystem{}
		wp := &stubWorkspacePolicy{}

		_, err := Register(r, sp, bus, exec, fs, wp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "register tool")
		// First tool without opts is "list_symbols"
		assert.Contains(t, err.Error(), "list_symbols")
		assert.Contains(t, err.Error(), "simulated registration failure")
	})
}

func TestRegister_Success(t *testing.T) {
	t.Parallel()

	r := &stubRegistry{} // no errors
	sp := &mockSecurityProvider{}
	bus := &stubEventBus{}
	exec := &mockExecutor{}
	fs := &stubFileSystem{}
	wp := &stubWorkspacePolicy{}

	result, err := Register(r, sp, bus, exec, fs, wp)
	require.NoError(t, err)
	require.NotNil(t, result, "expected a non-nil handler (VerifyArchitecture) on success")
}

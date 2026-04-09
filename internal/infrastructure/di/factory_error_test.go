// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	infra_tools "github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockFailingFS struct {
	infra_persistence.FileSystem
	mkdirErr error
	openErr  error
	statErr  error
}

func (m *mockFailingFS) MkdirAll(path string, perm os.FileMode) error {
	return m.mkdirErr
}

func (m *mockFailingFS) OpenFile(name string, flag int, perm os.FileMode) (infra_persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	return nil, errors.New("not implemented")
}

func (m *mockFailingFS) Stat(name string) (os.FileInfo, error) {
	if m.statErr != nil {
		return nil, m.statErr
	}
	return nil, os.ErrNotExist
}

func (m *mockFailingFS) CreateTemp(dir, pattern string) (infra_persistence.File, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	return nil, errors.New("not implemented")
}

func (m *mockFailingFS) ReadFile(name string) ([]byte, error) {
	if m.openErr != nil {
		return nil, m.openErr
	}
	return nil, errors.New("not implemented")
}

func TestGetHistoryManager_FailurePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("disk failure")

	tests := []struct {
		name    string
		fs      *mockFailingFS
		wantErr error
	}{
		{
			name: "DirectoryCreationFailure",
			fs: &mockFailingFS{
				mkdirErr: simulatedErr,
			},
			wantErr: errInfraInit,
		},
		{
			name: "BuildHistoryManagerFailure",
			fs: &mockFailingFS{
				openErr: simulatedErr, // simulate error during history manager build
				statErr: simulatedErr, // force load to fail instead of just 'not found'
			},
			wantErr: errInfraInit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)
			b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, tt.fs, nil)
			cfg := &config.Config{Mode: "assistant"}

			hManager, err := b.GetHistoryManager(ctx, cfg)
			assert.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, hManager)
		})
	}
}

func TestBuildTelemetry_Fallback(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("failed to open file")

	fs := &mockFailingFS{
		openErr: simulatedErr,
	}

	sm := new(mockConfigurableSecurityManager)
	factory := newTelemetryFactory(tempDir, fs, sm, nil)

	paths := &persistence.Paths{
		TurnsLogPath: "turns.log",
	}
	cfg := &config.Config{Model: "test-model"}

	dummyCleanup := func(ctx context.Context) error { return nil }
	pricingData, tracker, turnsLogger, cleanup := factory.BuildTelemetry(ctx, paths, cfg, dummyCleanup)

	assert.NotNil(t, pricingData)
	assert.NotNil(t, tracker)
	assert.NotNil(t, turnsLogger)
	assert.NotNil(t, cleanup)

	// Verify it's a No-Op turns logger
	_, ok := turnsLogger.(*ports.NoOpTurnsLogger)
	assert.True(t, ok, "Expected NoOpTurnsLogger on initialization failure")
}

func TestBuildSession_FailurePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("io error")

	tests := []struct {
		name    string
		setup   func(f *defaultSessionFactory)
		wantErr error
	}{
		{
			name: "EnsureDirectoriesFailure",
			setup: func(f *defaultSessionFactory) {
				f.FileSystem = &mockFailingFS{mkdirErr: simulatedErr}
			},
			wantErr: errInfraInit,
		},
		{
			name: "SessionStateInitializationFailure",
			setup: func(f *defaultSessionFactory) {
				f.FileSystem = &infra_persistence.OSFileSystem{}
				f.NewSessionState = func(ctx context.Context, modeDir string) (ports.SessionProvider, error) {
					return nil, simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)
			setupDefaultSMExpectations(sm)

			// Initialize with a real but dummy filesystem to avoid nil panics in EnsureDirectories
			factory := newSessionFactory(tempDir, &infra_persistence.OSFileSystem{}, sm, io.Discard, io.Discard, nil, nil, nil).(*defaultSessionFactory)
			if tt.setup != nil {
				tt.setup(factory)
			}

			cfg := &config.Config{Mode: "assistant"}
			sp, paths, cleanup, err := factory.BuildSession(ctx, cfg, "config.yaml", false, nil)

			assert.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, sp)
			assert.Nil(t, paths)
			assert.Nil(t, cleanup)
		})
	}
}

func TestBuildRegistry_FailurePaths(t *testing.T) {
	tempDir := t.TempDir()
	simulatedErr := errors.New("registration error")

	tests := []struct {
		name    string
		setup   func(f *defaultToolchainFactory)
		wantErr error
	}{
		{
			name: "RegisterAllToolsFailure",
			setup: func(f *defaultToolchainFactory) {
				f.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error {
					return simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
		{
			name: "RegisterMetricsFailure",
			setup: func(f *defaultToolchainFactory) {
				f.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error { return nil }
				f.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile, model, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
					return simulatedErr
				}
			},
			wantErr: errInfraInit,
		},
		{
			name: "RegisterPolicyToolsFailure",
			setup: func(f *defaultToolchainFactory) {
				f.RegisterAllTools = func(params infra_tools.ToolRegistrationParams) error { return nil }
				f.RegisterMetrics = func(r tools.Registry, sm security.Manager, logFile, traceFile, model, mode string, pricingOverrides map[string]pricing.ModelPricing, kvStore ports.KVStore) error {
					return nil
				}
				// Policy tools registration is done via SM
			},
			wantErr: errInfraInit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := new(mockConfigurableSecurityManager)
			if tt.name == "RegisterPolicyToolsFailure" {
				sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(simulatedErr)
			} else {
				sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
			}

			factory := newToolchainFactory(tempDir, nil, sm, nil, nil).(*defaultToolchainFactory)
			if tt.setup != nil {
				tt.setup(factory)
			}

			mockSP := new(mockSessionProvider)
			mockKV := new(mockKVStore)
			mockSP.On("GetSettings").Return(mockKV).Maybe()

			params := toolchainParams{
				Paths:           &persistence.Paths{},
				SessionProvider: mockSP,
			}

			reg, err := factory.BuildRegistry(params)
			assert.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, reg)
		})
	}
}

func TestGetUnifiedHistoryProvider_FailurePaths(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	simulatedErr := errors.New("mkdir failure")

	fs := &mockFailingFS{
		mkdirErr: simulatedErr,
	}

	sm := new(mockConfigurableSecurityManager)
	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, fs, nil)
	cfg := &config.Config{Mode: "assistant"}

	hManager := history.NewManager(nil, "history.jsonl", "archive.jsonl")
	provider, err := b.GetUnifiedHistoryProvider(ctx, cfg, hManager)

	assert.Error(t, err)
	assert.ErrorIs(t, err, errInfraInit)
	assert.Nil(t, provider)
}
func TestGetSuggestionService_Fallback(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Create an invalid directory structure to force NewGlobalPromptTracker to fail.
	// NewGlobalPromptTracker tries to create a file in homeDir/.tellmego/prompts.jsonl
	// If .tellmego is a file, it will fail.
	err := os.WriteFile(filepath.Join(tempDir, ".tellmego"), []byte(""), 0644)
	assert.NoError(t, err)

	sm := new(mockConfigurableSecurityManager)
	fs := &infra_persistence.OSFileSystem{}
	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, fs, nil)

	svc, err := b.GetSuggestionService(ctx, []string{"test"})
	assert.NoError(t, err)
	assert.NotNil(t, svc)
}

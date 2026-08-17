// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func TestSessionFactory_ErrorWrappingFormat(t *testing.T) {
	innerErr := errors.New("some security error")
	wrapped := fmt.Errorf("%w: security setup: %w", errInfraInit, innerErr)

	if !errors.Is(wrapped, errInfraInit) {
		t.Error("errors.Is(wrapped, errInfraInit) should be true")
	}
	if !errors.Is(wrapped, innerErr) {
		t.Error("errors.Is(wrapped, innerErr) should be true")
	}

	msg := wrapped.Error()
	if !strings.Contains(msg, "security setup") {
		t.Errorf("error message should contain 'security setup', got: %s", msg)
	}
	if strings.Contains(msg, "paths") {
		t.Errorf("error message should not contain raw paths, got: %s", msg)
	}
}

func TestDefaultSessionFactory_SetupSecurity_RegistersSkillsPaths(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, "config.yaml")
	var registeredReadOnly []string
	var registeredSafe []string

	sm := &mockConfigurableSecurityManager{
		RegisterReadOnlyPathFunc: func(path string) {
			registeredReadOnly = append(registeredReadOnly, path)
		},
		RegisterSafePathFunc: func(path string) {
			registeredSafe = append(registeredSafe, path)
		},
	}
	f := &defaultSessionFactory{
		HomeDir: homeDir,
		SM:      sm,
	}

	err := f.setupSecurity(&persistence.Paths{}, configPath)
	if err != nil {
		t.Fatalf("setupSecurity failed: %v", err)
	}

	expectedReadOnly := []string{
		configPath,
		filepath.Join(homeDir, "docs", "skills"),
		filepath.Join(homeDir, ".skills"),
	}

	for _, expected := range expectedReadOnly {
		found := false
		for _, p := range registeredReadOnly {
			if p == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected read-only path %q to be registered, registered: %v", expected, registeredReadOnly)
		}
	}

	expectedSafe := filepath.Join(homeDir, "output")
	foundSafe := false
	for _, p := range registeredSafe {
		if p == expectedSafe {
			foundSafe = true
			break
		}
	}
	if !foundSafe {
		t.Errorf("expected safe path %q to be registered, registered: %v", expectedSafe, registeredSafe)
	}
}

// TestHandleNewSession_RecordSessionCostWarning covers the warning path at
// session_factory.go:126-128: when telemetry.RecordSessionCost fails (e.g.
// the backup log is missing/corrupt), handleNewSession prints a warning to
// Stderr and continues instead of failing.
func TestHandleNewSession_RecordSessionCostWarning(t *testing.T) {
	ctx := context.Background()

	// A directory at the LogPath is a "corrupt" log: os.Open succeeds on the
	// directory but the scan read fails with EISDIR — a non-IsNotExist error,
	// so RecordSessionCost propagates the parse failure (a missing log file,
	// by contrast, is swallowed by os.IsNotExist in resolveUsageForSummary
	// and returns nil). Same directory-where-a-file-should-be pattern as
	// TestBootstrapper_Initialize_Errors/FailsOnStateInitError.
	logPath := filepath.Join(t.TempDir(), "logdir")
	if err := os.MkdirAll(logPath, 0755); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", logPath, err)
	}

	var stderr bytes.Buffer
	factory := &defaultSessionFactory{
		HomeDir:    t.TempDir(),
		FileSystem: &infra_persistence.OSFileSystem{},
		SM:         new(mockConfigurableSecurityManager),
		Stdout:     io.Discard,
		Stderr:     &stderr,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		RotateSession: func(ctx context.Context, fs infra_persistence.FileSystem, stdout io.Writer, paths persistence.Paths, retentionDays int, logger *slog.Logger) error {
			return nil
		},
	}

	paths := &persistence.Paths{LogPath: logPath}
	testCfg := &config.Config{Mode: "assistant", Model: "test-model"}
	mockKV := &mockKVStore{}

	if err := factory.handleNewSession(ctx, paths, testCfg, nil, mockKV); err != nil {
		t.Fatalf("handleNewSession returned error on warning path: %v", err)
	}
	if !strings.Contains(stderr.String(), "Failed to record session cost") {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), "Failed to record session cost")
	}
}

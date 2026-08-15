// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
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

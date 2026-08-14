// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// TestBuildSuggestionService_TrackerInitFailure verifies the graceful
// degradation contract of BuildSuggestionService: when the global prompt
// tracker cannot be initialized (MkdirAll on <homeDir>/output fails because
// a plain file named "output" occupies the path), the function logs a
// warning and substitutes history.NewNoOpTracker() instead of failing.
//
// The tracker-failure trigger reuses the proven real-FS technique from
// TestNewGlobalPromptTracker_MkdirError (internal/infrastructure/history):
// no custom FS double — a plain file blocks the MkdirAll.
func TestBuildSuggestionService_TrackerInitFailure(t *testing.T) {
	homeDir := t.TempDir()
	conflictFile := filepath.Join(homeDir, "output")
	if err := os.WriteFile(conflictFile, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	// Real OS-backed filesystem adapter, constructed exactly as the production
	// composition root does (internal/infrastructure/di/container.go:50).
	fs := &infra_persistence.OSFileSystem{}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	var wp services.WorkspacePolicy // zero value (nil interface) — unused on this path

	service, err := BuildSuggestionService(context.Background(), fs, homeDir, io.Discard, logger, wp, nil)
	if err != nil {
		t.Fatalf("BuildSuggestionService must degrade gracefully on tracker init failure, got error: %v", err)
	}
	if service == nil {
		t.Fatal("expected non-nil suggestion service")
	}

	if !strings.Contains(logBuf.String(), "failed to initialize global prompt tracker") {
		t.Errorf("expected warning about tracker init failure, got log: %q", logBuf.String())
	}
}

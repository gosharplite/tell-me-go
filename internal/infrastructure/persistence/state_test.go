// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func verifyStateInitialization(t *testing.T, state ports.SessionProvider) {
	t.Helper()
	if state.GetTasks() == nil {
		t.Error("expected all services to be initialized")
	}
}

func TestNewSessionState_FileStorage(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	ctx := context.Background()

	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	verifyStateInitialization(t, state)

	info := state.GetInfo()
	if info.Env["STORAGE_TYPE"] != "sqlite" {
		t.Errorf("expected STORAGE_TYPE to be sqlite, got %s", info.Env["STORAGE_TYPE"])
	}

	if info.Paths["config_dir"] != tempDir {
		t.Errorf("expected config_dir to be %s, got %s", tempDir, info.Paths["config_dir"])
	}
}

func TestNewSessionState_MemoryStorage(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	verifyStateInitialization(t, state)

	info := state.GetInfo()
	if info.Env["STORAGE_TYPE"] != "memory" {
		t.Errorf("expected STORAGE_TYPE to be memory, got %s", info.Env["STORAGE_TYPE"])
	}
}

func TestSessionState_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// 1. Create a session and set info
	state1, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}

	info := state1.GetInfo()
	info.ActiveToolkits = []string{"git", "k8s"}
	state1.SetInfo(info)
	_ = state1.Close()

	// 2. Reload the session state from disk
	state2, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state2.Close() }()

	// 3. Verify ActiveToolkits is restored
	restoredInfo := state2.GetInfo()
	if len(restoredInfo.ActiveToolkits) != 2 {
		t.Fatalf("expected 2 active toolkits, got %d", len(restoredInfo.ActiveToolkits))
	}

	tkMap := make(map[string]bool)
	for _, tk := range restoredInfo.ActiveToolkits {
		tkMap[tk] = true
	}

	if !tkMap["git"] || !tkMap["k8s"] {
		t.Errorf("restored toolkits missing git or k8s: %v", restoredInfo.ActiveToolkits)
	}
}

func TestSessionState_GetSettings(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	if state.GetSettings() == nil {
		t.Error("expected GetSettings() to return a non-nil KVStore")
	}
}

func TestSessionState_GetHealthChecker_SQLite(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	ctx := context.Background()

	// Default STORAGE_TYPE is "sqlite" — creates a real DB.
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	checker := state.GetHealthChecker()

	// Verify it's the real health checker, not the no-op fallback.
	if checker == nil {
		t.Fatal("expected non-nil health checker")
	}
	if _, ok := checker.(*sqliteHealthChecker); !ok {
		t.Errorf("expected *sqliteHealthChecker, got %T", checker)
	}
}

func TestSessionState_GetHealthChecker_Memory(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	t.Setenv("STORAGE_TYPE", "memory")
	state, err := NewSessionState(ctx, tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = state.Close() }()

	checker := state.GetHealthChecker()
	if checker == nil {
		t.Fatal("expected non-nil health checker")
	}
	if _, ok := checker.(*noOpHealthChecker); !ok {
		t.Errorf("expected *noOpHealthChecker, got %T", checker)
	}
}

func TestNewSessionState_InitRepositoriesFailure(t *testing.T) {
	t.Parallel()

	// Use a path where the parent directory does not exist.
	// initSQLiteDB will succeed at sql.Open but ExecContext will fail
	// because the driver cannot create the DB file in a nonexistent directory.
	badDir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	ctx := context.Background()

	_, err := NewSessionState(ctx, badDir)
	if err == nil {
		t.Error("expected error from NewSessionState with invalid path, got nil")
	}
}

// failingTaskStore is a ListStore[ports.Task] whose ReadAll always fails.
type failingTaskStore struct{}

func (s *failingTaskStore) ReadAll(ctx context.Context) ([]ports.Task, error) {
	return nil, errors.New("simulated read failure")
}
func (s *failingTaskStore) Update(ctx context.Context, id float64, item ports.Task) error { return nil }
func (s *failingTaskStore) Delete(ctx context.Context, id float64) error                  { return nil }
func (s *failingTaskStore) DeleteAll(ctx context.Context) error                           { return nil }
func (s *failingTaskStore) Append(ctx context.Context, item ports.Task) error             { return nil }
func (s *failingTaskStore) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]ports.Task, error) {
	return nil, errors.New("simulated query failure")
}
func (s *failingTaskStore) Count(ctx context.Context) (int, error) {
	return 0, errors.New("simulated count failure")
}

func TestInitServices_InitializeFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := initServices(ctx, &failingTaskStore{})
	if err == nil {
		t.Fatal("expected error from initServices when Initialize fails, got nil")
	}
	if !strings.Contains(err.Error(), "simulated query failure") {
		t.Errorf("expected error to contain 'simulated query failure', got: %v", err)
	}
}
